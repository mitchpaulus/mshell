package main

import (
	"slices"
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/term"
)

type previewRequest struct {
	path       string
	entry      os.DirEntry
	maxLines   int
	onlineOnly bool // file data is only in the cloud; do not read it
	gen        uint64
	// Pixel size that an image preview must fit in. Zero when images are
	// not drawn.
	imageW int
	imageH int
	// Width over height of one image pixel on screen.
	pixelAspect float64
}

type previewResult struct {
	path  string
	lines []string
	image imagePreview // drawn below the lines, if the file is an image
	gen   uint64
}

// imagePreview is an image preview encoded as sixel data.
type imagePreview struct {
	sixel  []byte
	imageW int // the pixel width the image was fit to
}

// folderStateRequest asks the folder state worker for the sync status of the
// named folders inside dir.
type folderStateRequest struct {
	dir   string
	names []string
}

// folderStateRenderInterval limits how often arriving folder states redraw the screen.
const folderStateRenderInterval = 30 * time.Millisecond

type archiveListingEntry struct {
	name     string
	isDir    bool
	size     int64
	modified time.Time
}

type fileManagerVolumeEntry struct {
	name string
}

func (e fileManagerVolumeEntry) Name() string               { return e.name }
func (e fileManagerVolumeEntry) IsDir() bool                { return true }
func (e fileManagerVolumeEntry) Type() fs.FileMode          { return fs.ModeDir }
func (e fileManagerVolumeEntry) Info() (fs.FileInfo, error) { return nil, nil }

// OneDrive may create this hidden lock/metadata file inside synced directories.
// Hide it from the file manager so it does not clutter listings or previews.
const oneDriveHiddenMetadataFileName = ".849C9593-D756-4E56-8D6E-42412F2A707B"

// Windows file attributes set by the cloud files driver (OneDrive and other
// sync providers). They are reported in directory listings, so reading them
// never downloads the file.
const (
	fileAttributeOffline            = 0x1000
	fileAttributePinned             = 0x80000
	fileAttributeRecallOnDataAccess = 0x400000
)

// cloudFileState describes the sync status of a file or folder in a cloud sync folder.
type cloudFileState int

const (
	cloudFileNotManaged cloudFileState = iota // not in a cloud sync folder, or no status
	cloudFileOnlineOnly                       // only in the cloud; reading it downloads it
	cloudFileLocal                            // downloaded; the provider may free it later
	cloudFilePinned                           // "Always keep on this device"
	cloudFileSyncing                          // upload or download pending or in progress
	cloudFileError                            // the provider reports an error or warning
)

// cloudFileStateFromAttributes classifies a file from its Windows attributes.
// inSyncRoot says whether the containing folder is managed by a sync provider,
// which is the only way to tell a downloaded cloud file from a plain local file.
func cloudFileStateFromAttributes(attrs uint32, isDir bool, inSyncRoot bool) cloudFileState {
	// A pinned file that is still downloading has both the pinned and the
	// recall bits. Treat it as online only until its data is here.
	if !isDir && attrs&(fileAttributeRecallOnDataAccess|fileAttributeOffline) != 0 {
		return cloudFileOnlineOnly
	}
	if attrs&fileAttributePinned != 0 {
		return cloudFilePinned
	}
	if inSyncRoot && !isDir {
		return cloudFileLocal
	}
	return cloudFileNotManaged
}

// cloudFileStateFromStorageProvider maps the shell's System.StorageProviderState
// value (the status icon Explorer shows) to a cloudFileState. Folder attributes
// say nothing about the files inside, so this is how folders get a status.
func cloudFileStateFromStorageProvider(value uint32) cloudFileState {
	switch value {
	case 1: // STORAGEPROVIDERSTATE_SPARSE
		return cloudFileOnlineOnly
	case 2: // STORAGEPROVIDERSTATE_IN_SYNC
		return cloudFileLocal
	case 3: // STORAGEPROVIDERSTATE_PINNED
		return cloudFilePinned
	case 4, 5, 6, 10: // PENDING_UPLOAD, PENDING_DOWNLOAD, TRANSFERRING, PENDING_UNSPECIFIED
		return cloudFileSyncing
	case 7, 8: // ERROR, WARNING
		return cloudFileError
	}
	return cloudFileNotManaged // NONE, EXCLUDED, or unknown
}

func (fm *FileManager) cloudState(entry os.DirEntry) cloudFileState {
	if fm.showingWindowsVolumes {
		return cloudFileNotManaged
	}
	if entry.IsDir() {
		if state, ok := fm.folderCloudStates[fm.selectedEntryPath(entry)]; ok {
			return state
		}
	}
	return cloudFileStateFromAttributes(fileAttributes(entry), entry.IsDir(), fm.inCloudSyncRoot)
}

// cloudStateMarker returns the marker and its color for the left pane.
// Terminals disagree on how wide the cloud is, so the renderer gives the marker
// a fixed slot and moves the cursor past it explicitly.
func cloudStateMarker(state cloudFileState) (string, string) {
	switch state {
	case cloudFileOnlineOnly:
		return "\u2601", "\033[36m" // cloud, cyan
	case cloudFileLocal:
		return "\u2713", "\033[32m" // check, green
	case cloudFilePinned:
		return "\u25cf", "\033[32m" // filled circle, green
	case cloudFileSyncing:
		return "\u21bb", "\033[34m" // clockwise arrow, blue
	case cloudFileError:
		return "\u2717", "\033[31m" // cross, red
	}
	return "", ""
}

// cloudMarkerCols is the width of the left margin plus the marker slot. The
// slot is three columns because Windows Terminal draws the cloud with the color
// emoji font, which spills past two columns. The space between the marker and
// the name belongs to the name, so it takes the name's highlight on the
// selected row.
const cloudMarkerCols = 4

// sizeCols is the width of the size column at the right of the left pane: a
// space, then the size right aligned in four columns.
const sizeCols = 5

// minNameColsWithSize is the narrowest name column that still gets a size
// column. On a narrower pane the size is dropped so names stay readable.
const minNameColsWithSize = 8

type FileManager struct {
	rows, cols int
	stdInFd    int
	oldState   term.State

	currentDir string
	entries    []os.DirEntry
	cursor     int
	offset     int
	showingWindowsVolumes bool
	inCloudSyncRoot       bool // currentDir is managed by OneDrive or another sync provider

	// Folder sync status comes from a shell lookup that is too slow to run
	// while rendering, so a background worker fills this map by folder path.
	folderCloudStates     map[string]cloudFileState
	folderStateReqCh      chan folderStateRequest
	folderStatesRequested bool // the current listing's folders were sent to the worker

	// File sizes in bytes by path, -1 when the size cannot be read. Filled as
	// entries are drawn, since sizes need a stat call per file outside
	// Windows. Cached until the directory is reloaded.
	entrySizes map[string]int64

	// Directory summary for the bottom row. The total size is shown only once
	// every file's size is in entrySizes: always on Windows, where sizes come
	// with the directory listing, and elsewhere once every file has been drawn.
	dirCount      int
	fileCount     int
	knownSizes    int   // files with an entry in entrySizes
	totalFileSize int64 // sum of the sizes in entrySizes

	hostname string
	username string

	previewCache  map[string][]string // cached preview lines per file path
	previewReqCh  chan previewRequest  // sends requests to the preview worker
	previewChan   chan previewResult   // receives results from the preview worker
	previewDone   chan struct{}        // closed, under renderMu, to shut down background goroutines
	previewWG     sync.WaitGroup       // the preview receiver; the workers are not waited for
	previewGen    uint64               // bumped on selection change; stale results are dropped
	previewImages map[string]imagePreview // cached image per file path, for image files
	// Paths in the order their images were cached, oldest first, and the
	// total size of the cached images. Used to drop the oldest images once
	// the total passes maxImageCacheBytes.
	previewImageOrder []string
	previewImageBytes int

	sixel sixelTerminal // whether and how images can be drawn

	// Search state
	searchActive bool   // true when a search has been committed
	searchTerm   string // lowercase committed search term
	searchMatches []int // indices into entries that match

	ttyOut *os.File // where TUI output goes (may differ from stdout)

	bookmarks map[byte]string

	// Status message (shown once at bottom, cleared on first keypress)
	statusMsg string

	// The evaluator whose key bindings and definitions the file manager
	// uses, and the context its quotes run in (FileManagerInput.go).
	ev          *EvalState
	context     ExecuteContext
	definitions []MShellDefinition

	// input is what the terminal sent, read with the shell's lexer, and
	// keys what the key resolver keeps between keys.
	input *StdinReaderState
	keys  keyResolver[fmSnapshot, fmMode, *fmBindingEntry]
	mode  fmMode
	// popup is set while the pending keys wait for more, and the bindings
	// that can follow are listed.
	popup bool
	// asking is set while a question's box is up.
	asking     bool
	line       fmLine
	helpOffset int

	// The selection: absolute paths, in the order they were selected.
	selected    []string
	selectedSet map[string]bool

	// startedIn is the working directory when the file manager started.
	// quit is set by fmQuit and fmQuitNoCd, quitNoCd by fmQuitNoCd, and
	// exitResult when a quote called exit.
	startedIn  string
	quit       bool
	quitNoCd   bool
	exitResult *EvalResult

	// What the file manager turned on in the terminal, to turn off before
	// another program gets it.
	hasTerminal     bool
	bracketedPaste  bool
	kittyKeys       bool
	kittyKeysPushed bool

	// renderMu serializes render() calls between the main loop and the
	// preview receiver goroutine.
	renderMu sync.Mutex
}

// RunFileManager runs as a standalone subcommand (msh fm). It loads the
// startup files, as a script does, so the user's bindings work. The TUI
// goes to /dev/tty, and the final directory is printed to stdout. If
// startDir is non-empty and is a valid directory, it is used instead of
// the working directory.
func RunFileManager(startDir string) int {
	// Open /dev/tty for TUI output so stdout stays clean for the directory result.
	ttyOut := os.Stdout
	if runtime.GOOS != "windows" {
		if tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0); err == nil {
			ttyOut = tty
			defer tty.Close()
		}
	}

	ev, context, definitions, problem := loadFileManagerStartup()
	fm, err := newFileManager(ev, context, definitions, ttyOut, startDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if problem != "" && fm.statusMsg == "" {
		fm.statusMsg = problem
	}
	if err := fm.run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if fm.exitResult != nil {
		return toProcessExitStatus(fm.exitResult.ExitCode)
	}

	// Print the final directory to stdout so callers can cd to it.
	if fm.quitNoCd {
		fmt.Fprintln(os.Stdout, fm.startedIn)
	} else {
		fmt.Fprintln(os.Stdout, fm.currentDir)
	}
	return 0
}

// loadFileManagerStartup loads the startup files for msh fm, with their
// standard output on standard error, so the directory printed at the end is
// the only standard output. A problem with them is returned to be shown at
// the bottom; the file manager starts anyway, with what loaded.
func loadFileManagerStartup() (*EvalState, ExecuteContext, []MShellDefinition, string) {
	ev := &EvalState{PositionalArgs: []string{}, CallStack: make(CallStack, 0, 10)}
	ev.EnvironmentHistory()
	context := ExecuteContext{Variables: map[string]MShellObject{}, Pbm: NewPathBinManager()}
	PrebuildCoreBuiltins()
	startup := context
	startup.StandardOutput = os.Stderr
	stack := MShellStack{}
	definitions, err := stdLibDefinitions(&stack, startup, ev)
	problem := ""
	if err != nil {
		fmt.Fprintln(os.Stderr, formatStartupErrorMessage(err, "", "", 0, 0))
		problem = "Error loading startup files: " + firstLine(err.Error())
	}
	for _, e := range ev.StartupTypeErrors {
		fmt.Fprintln(os.Stderr, e)
	}
	if len(ev.StartupTypeErrors) > 0 && problem == "" {
		problem = "Type errors in the startup files' top-level code, which did not run."
	}
	ev.printAbbrWarnings()
	return ev, context, definitions, problem
}

// RunFileManagerBuiltin runs from a builtin call during evaluation, with
// the evaluator's bindings and definitions. The terminal is in cooked
// mode, so this handles MakeRaw/Restore itself. If startDir is non-empty
// and is a valid directory, it is used instead of cwd. It returns the
// directory to change to, which is "" after fmQuitNoCd, and the result of
// exit when a quote called it.
func RunFileManagerBuiltin(ev *EvalState, context ExecuteContext, definitions []MShellDefinition, startDir string) (string, *EvalResult) {
	fm, err := newFileManager(ev, context, definitions, os.Stdout, startDir)
	if err != nil {
		return "", nil
	}
	if err := fm.run(); err != nil {
		return "", nil
	}
	if fm.exitResult != nil {
		return "", fm.exitResult
	}
	if fm.quitNoCd {
		return "", nil
	}
	return fm.currentDir, nil
}

// newFileManager makes a file manager that draws on ttyOut and starts in
// startDir, or else the working directory.
func newFileManager(ev *EvalState, context ExecuteContext, definitions []MShellDefinition, ttyOut *os.File, startDir string) (*FileManager, error) {
	fm := &FileManager{
		ev:          ev,
		context:     context,
		definitions: definitions,
		ttyOut:      ttyOut,
		stdInFd:     int(os.Stdin.Fd()),
		input:       &StdinReaderState{array: make([]byte, 1024), read: os.Stdin.Read},
	}
	cols, rows, err := term.GetSize(int(ttyOut.Fd()))
	if err != nil {
		return nil, fmt.Errorf("Error getting terminal size: %s", err)
	}
	fm.rows = rows
	fm.cols = cols

	fm.initUserInfo()
	fm.bookmarks = loadBookmarks()

	fm.startedIn, err = os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("Error getting working directory: %s", err)
	}
	fm.currentDir = fm.startedIn
	if startDir != "" {
		if info, err := os.Stat(startDir); err == nil && info.IsDir() {
			fm.currentDir, _ = filepath.Abs(startDir)
		} else {
			fm.statusMsg = fmt.Sprintf("Directory not found: %s", startDir)
		}
	}
	fm.loadDirectory()
	return fm, nil
}

// run takes the terminal and runs the file manager until it ends.
func (fm *FileManager) run() error {
	if err := fm.takeTerminal(); err != nil {
		return fmt.Errorf("Error entering raw mode: %s", err)
	}
	defer fm.releaseTerminal()
	fm.mainLoop()
	return nil
}

func (fm *FileManager) initUserInfo() {
	fm.hostname, _ = os.Hostname()
	if u, err := user.Current(); err == nil {
		fm.username = u.Username
	} else {
		fm.username = os.Getenv("USER")
	}
}

func (fm *FileManager) startPreviewLoop() {
	fm.previewReqCh = make(chan previewRequest, 1)
	fm.previewChan = make(chan previewResult, 1)
	fm.previewDone = make(chan struct{})

	// Worker goroutine: computes previews, coalescing rapid requests.
	// Shutdown does not wait for it: a read can block for a long time (a file
	// being downloaded, a network drive that stopped responding) and Go cannot
	// cancel it. When the read returns, the worker sees previewDone and exits.
	go func() {
		for {
			select {
			case <-fm.previewDone:
				return
			case req := <-fm.previewReqCh:
				// Drain and keep only the newest request.
				for {
					select {
					case newer := <-fm.previewReqCh:
						req = newer
					default:
						goto COMPUTE
					}
				}
			COMPUTE:
				lines, image := makePreview(req)
				select {
				case <-fm.previewDone:
					return
				case fm.previewChan <- previewResult{
					path:  req.path,
					lines: lines,
					image: image,
					gen:   req.gen,
				}:
				}
			}
		}
	}()

	// Receiver goroutine: applies results and re-renders.
	fm.previewWG.Add(1)
	go func() {
		defer fm.previewWG.Done()
		for {
			select {
			case <-fm.previewDone:
				return
			case result := <-fm.previewChan:
				fm.renderMu.Lock()
				if !fm.backgroundStopped() && result.gen == fm.previewGen {
					fm.previewCache[result.path] = result.lines
					if result.image.sixel != nil {
						fm.cacheImagePreview(result.path, result.image)
					}
					fm.render()
				}
				fm.renderMu.Unlock()
			}
		}
	}()
}

// makePreview computes the preview for a request. Tests replace it to
// simulate previews that block.
var makePreview = func(req previewRequest) ([]string, imagePreview) {
	if !req.onlineOnly && !req.entry.IsDir() && looksCloudOnly(req.path) {
		req.onlineOnly = true
	}
	if req.imageW > 0 && !req.onlineOnly && !req.entry.IsDir() && isImagePreviewPath(req.path) {
		lines, sixel := previewImage(req.path, req.imageW, req.imageH, req.pixelAspect)
		return lines, imagePreview{sixel: sixel, imageW: req.imageW}
	}
	return computePreview(req.entry, req.path, req.maxLines, req.onlineOnly), imagePreview{}
}

// storageProviderStateLookupFactory creates the folder status lookup. Tests
// replace it to simulate lookups that fail, hang, or panic.
var storageProviderStateLookupFactory = newStorageProviderStateLookup

// startFolderStateWorker looks up the sync status of folders in cloud sync
// folders and re-renders as results arrive. It stops when previewDone is
// closed. Shutdown does not wait for it, because a COM call into the sync
// provider can hang (for example while OneDrive is unresponsive). If that
// happens, folder markers stop appearing, the file manager keeps working, and
// the goroutine exits whenever the call returns.
func (fm *FileManager) startFolderStateWorker() {
	fm.folderStateReqCh = make(chan folderStateRequest, 1)
	newLookup := storageProviderStateLookupFactory
	go func() {
		// Folder markers are optional. A panic here turns them off for the rest
		// of this session instead of taking down the shell.
		defer func() { _ = recover() }()

		// COM is set up per OS thread, so keep this goroutine on one thread.
		// The thread is left locked so it is discarded when the goroutine exits.
		runtime.LockOSThread()
		lookup, closeLookup := newLookup()
		if lookup == nil {
			return
		}
		defer closeLookup()

		var pending *folderStateRequest
		for {
			var req folderStateRequest
			if pending != nil {
				req, pending = *pending, nil
			} else {
				select {
				case <-fm.previewDone:
					return
				case req = <-fm.folderStateReqCh:
				}
			}

			// Stop early when shutting down or when the user has moved to
			// another directory; a newer request is picked up on the next loop.
			stop := func() bool {
				select {
				case <-fm.previewDone:
					return true
				case newer := <-fm.folderStateReqCh:
					pending = &newer
					return true
				default:
					return false
				}
			}

			var lastRender time.Time
			dirty := false
			found := func(name string, value uint32) {
				path := filepath.Join(req.dir, name)
				state := cloudFileStateFromStorageProvider(value)
				fm.renderMu.Lock()
				defer fm.renderMu.Unlock()
				if fm.backgroundStopped() {
					return
				}
				if old, seen := fm.folderCloudStates[path]; seen && old == state {
					return
				}
				fm.folderCloudStates[path] = state
				dirty = true
				if time.Since(lastRender) >= folderStateRenderInterval {
					fm.render()
					lastRender = time.Now()
					dirty = false
				}
			}
			lookup(req.dir, req.names, stop, found)

			func() {
				// Unlock with defer so a panic in render cannot leave the lock held.
				fm.renderMu.Lock()
				defer fm.renderMu.Unlock()
				if dirty && !fm.backgroundStopped() {
					fm.render()
				}
			}()
		}
	}()
}

// scheduleFolderStates sends the folders of the current listing to the folder
// state worker, once per directory load. Folders on screen are sent first.
func (fm *FileManager) scheduleFolderStates() {
	if fm.folderStatesRequested || fm.folderStateReqCh == nil {
		return
	}
	fm.folderStatesRequested = true
	if !fm.inCloudSyncRoot || fm.showingWindowsVolumes {
		return
	}
	if fm.folderCloudStates == nil {
		fm.folderCloudStates = make(map[string]cloudFileState)
	}
	req := folderStateRequest{dir: fm.currentDir}
	visibleEnd := fm.offset + fm.visibleRows()
	for i, entry := range fm.entries {
		if entry.IsDir() && i >= fm.offset && i < visibleEnd {
			req.names = append(req.names, entry.Name())
		}
	}
	for i, entry := range fm.entries {
		if entry.IsDir() && (i < fm.offset || i >= visibleEnd) {
			req.names = append(req.names, entry.Name())
		}
	}
	if len(req.names) == 0 {
		return
	}
	select {
	case fm.folderStateReqCh <- req:
	default:
		select {
		case <-fm.folderStateReqCh:
		default:
		}
		fm.folderStateReqCh <- req
	}
}

// stopPreviewLoop shuts down background work. previewDone is closed while
// holding renderMu, so once this returns no background goroutine will draw on
// the terminal again, even one that is still stuck in a slow call.
func (fm *FileManager) stopPreviewLoop() {
	fm.renderMu.Lock()
	close(fm.previewDone)
	fm.renderMu.Unlock()
	fm.previewWG.Wait()
}

// backgroundStopped reports whether stopPreviewLoop has run. Callers must hold
// renderMu.
func (fm *FileManager) backgroundStopped() bool {
	select {
	case <-fm.previewDone:
		return true
	default:
		return false
	}
}

// schedulePreview sends a preview request for the currently selected entry.
// Safe to call on every cursor/directory change; the worker coalesces rapid
// requests and stale results are dropped via the generation number.
func (fm *FileManager) schedulePreview() {
	if len(fm.entries) == 0 || fm.cursor >= len(fm.entries) {
		return
	}
	entry := fm.entries[fm.cursor]
	path := fm.selectedEntryPath(entry)

	imageW, imageH := fm.imagePreviewSize()
	if image, ok := fm.previewImages[path]; ok && image.imageW != imageW {
		// The preview pane changed width since the image was made.
		fm.dropImagePreview(path)
	}

	if _, ok := fm.previewCache[path]; ok {
		return // already cached
	}

	fm.previewGen++

	req := previewRequest{
		path:        path,
		entry:       entry,
		maxLines:    fm.visibleRows(),
		onlineOnly:  !entry.IsDir() && fm.cloudState(entry) == cloudFileOnlineOnly,
		gen:         fm.previewGen,
		imageW:      imageW,
		imageH:      imageH,
		pixelAspect: fm.sixel.pixelAspect,
	}

	// Non-blocking send; if the channel is full the worker will drain
	// and pick up the latest request.
	select {
	case fm.previewReqCh <- req:
	default:
		// Channel full — drain the old request and send the new one.
		select {
		case <-fm.previewReqCh:
		default:
		}
		fm.previewReqCh <- req
	}
}

// imagePreviewRow is the screen row where image previews start, below the
// header and the image's description line.
const imagePreviewRow = 3

// imagePreviewSize returns the size in pixels that an image preview must fit
// in, or zeros when images are not drawn. The image stops one row above the
// bottom of the screen: a sixel image that reaches the last row makes the
// terminal scroll.
func (fm *FileManager) imagePreviewSize() (int, int) {
	if !fm.sixel.supported {
		return 0, 0
	}
	cols := fm.cols - fm.leftPaneWidth() - 4 // separator and leading space
	rows := fm.rows - imagePreviewRow
	if cols <= 0 || rows <= 0 {
		return 0, 0
	}
	return min(cols*fm.sixel.cellW, maxImageSide), min(rows*fm.sixel.cellH, maxImageSide)
}

// cacheImagePreview stores an image preview, dropping the oldest cached
// images while the cache holds more than maxImageCacheBytes.
func (fm *FileManager) cacheImagePreview(path string, image imagePreview) {
	if fm.previewImages == nil {
		fm.previewImages = make(map[string]imagePreview)
	}
	fm.dropImagePreview(path)
	fm.previewImages[path] = image
	fm.previewImageBytes += len(image.sixel)
	fm.previewImageOrder = append(fm.previewImageOrder, path)
	for fm.previewImageBytes > maxImageCacheBytes && len(fm.previewImageOrder) > 1 {
		oldest := fm.previewImageOrder[0]
		fm.previewImageOrder = fm.previewImageOrder[1:]
		if oldest != path {
			fm.dropImagePreview(oldest)
		}
	}
}

// dropImagePreview removes a cached image and its preview text, so the next
// time the file is selected the preview is made again.
func (fm *FileManager) dropImagePreview(path string) {
	image, ok := fm.previewImages[path]
	if !ok {
		return
	}
	fm.previewImageBytes -= len(image.sixel)
	delete(fm.previewImages, path)
	delete(fm.previewCache, path)
}

// selectedImage returns the sixel data to draw for the selected entry, or nil
// if there is none or it was made for a different pane width.
func (fm *FileManager) selectedImage() []byte {
	if len(fm.entries) == 0 || fm.cursor >= len(fm.entries) {
		return nil
	}
	image, ok := fm.previewImages[fm.selectedEntryPath(fm.entries[fm.cursor])]
	if !ok {
		return nil
	}
	if imageW, _ := fm.imagePreviewSize(); image.imageW != imageW {
		return nil
	}
	return image.sixel
}

func (fm *FileManager) mainLoop() {
	if term.IsTerminal(fm.stdInFd) && term.IsTerminal(int(fm.ttyOut.Fd())) {
		detected, kitty := detectTerminal(fm.ttyOut, fm.stdInFd, fm.cols, fm.rows, os.Getenv("MSH_KITTY_KEYS") != "0")
		setting, set := os.LookupEnv(cellPixelsEnvVar)
		var message string
		fm.sixel, message = applyCellPixelsSetting(detected, setting, set)
		if message != "" && fm.statusMsg == "" {
			fm.statusMsg = message
		}
		fm.kittyKeys = kitty
		fm.setKittyKeys(true)
	}
	fm.startPreviewLoop()
	fm.startFolderStateWorker()
	defer fm.stopPreviewLoop()

	for {
		fm.renderMu.Lock()
		fm.schedulePreview()
		fm.scheduleFolderStates()
		fm.render()
		fm.renderMu.Unlock()

		if fm.handleNextInput() {
			return
		}
	}
}

func (fm *FileManager) loadDirectory() {
	fm.showingWindowsVolumes = false
	fm.inCloudSyncRoot = isCloudSyncRoot(fm.currentDir)
	fm.folderStatesRequested = false
	fm.resetEntrySizes()
	entries, err := os.ReadDir(fm.currentDir)
	if err != nil {
		fm.entries = nil
		return
	}
	entries = filterIgnoredFileManagerEntries(entries)

	sort.SliceStable(entries, func(i, j int) bool {
		iDir := entries[i].IsDir()
		jDir := entries[j].IsDir()
		if iDir != jDir {
			return iDir
		}
		return VersionSortComparer(strings.ToLower(entries[i].Name()), strings.ToLower(entries[j].Name())) < 0
	})

	fm.entries = entries
	fm.previewCache = make(map[string][]string)
	fm.previewImages = make(map[string]imagePreview)
	fm.previewImageOrder = nil
	fm.previewImageBytes = 0
	fm.searchActive = false
	fm.searchMatches = fm.searchMatches[:0]

	for _, entry := range entries {
		if entry.IsDir() {
			fm.dirCount++
			continue
		}
		fm.fileCount++
		// On Windows a file's size comes with the directory listing, so the
		// total costs nothing. A symbolic link shows its target's size, which
		// needs a stat call, so links wait until they are drawn.
		if runtime.GOOS == "windows" && entry.Type()&fs.ModeSymlink == 0 {
			fm.entryBytes(entry)
		}
	}
}

// resetEntrySizes clears the size cache and the directory summary.
func (fm *FileManager) resetEntrySizes() {
	fm.entrySizes = make(map[string]int64)
	fm.dirCount = 0
	fm.fileCount = 0
	fm.knownSizes = 0
	fm.totalFileSize = 0
}

func windowsVolumeRoot(name string) string {
	return name + `\`
}

func windowsVolumeName(path string) string {
	volumeName := filepath.VolumeName(path)
	if len(volumeName) == 2 && volumeName[1] == ':' {
		return strings.ToUpper(volumeName)
	}
	return ""
}

func isWindowsVolumeRoot(path string) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	volumeName := windowsVolumeName(path)
	if volumeName == "" {
		return false
	}
	return filepath.Clean(path) == filepath.Clean(windowsVolumeRoot(volumeName))
}

func (fm *FileManager) selectedEntryPath(entry os.DirEntry) string {
	if fm.showingWindowsVolumes {
		return windowsVolumeRoot(entry.Name())
	}
	return filepath.Join(fm.currentDir, entry.Name())
}

func (fm *FileManager) showWindowsVolumes() {
	fm.entries = mountedWindowsVolumes()
	fm.previewCache = make(map[string][]string)
	fm.previewImages = make(map[string]imagePreview)
	fm.previewImageOrder = nil
	fm.previewImageBytes = 0
	fm.resetEntrySizes()
	fm.searchActive = false
	fm.searchMatches = fm.searchMatches[:0]
	fm.showingWindowsVolumes = true
	fm.cursor = 0
	fm.offset = 0

	currentVolume := windowsVolumeName(fm.currentDir)
	for i, entry := range fm.entries {
		if entry.Name() == currentVolume {
			fm.cursor = i
			fm.adjustScroll()
			break
		}
	}
}

func (fm *FileManager) requireRealDirectory() bool {
	if !fm.showingWindowsVolumes {
		return true
	}
	fm.statusMsg = "Select a volume first."
	return false
}

// visibleRows is how many entries the listing shows: the rows below the
// header, less the prompt line and the message or summary at the bottom.
// In help mode it is how many lines of the help are shown.
func (fm *FileManager) visibleRows() int {
	switch {
	case fm.mode == fmModeHelp:
		return fm.helpRows()
	case fm.mode == fmModeInsert && fm.statusMsg != "":
		return fm.rows - 3 // header, message and prompt line
	case fm.mode == fmModeInsert || fm.statusMsg != "" || !fm.showingWindowsVolumes:
		return fm.rows - 2 // header + bottom bar
	}
	return fm.rows - 1 // header only
}

func (fm *FileManager) clampCursor() {
	if len(fm.entries) == 0 {
		fm.cursor = 0
		return
	}
	if fm.cursor < 0 {
		fm.cursor = 0
	}
	if fm.cursor >= len(fm.entries) {
		fm.cursor = len(fm.entries) - 1
	}
}

func (fm *FileManager) adjustScroll() {
	visible := fm.visibleRows()
	if visible <= 0 {
		return
	}
	if fm.cursor < fm.offset {
		fm.offset = fm.cursor
	}
	if fm.cursor >= fm.offset+visible {
		fm.offset = fm.cursor - visible + 1
	}
}

func (fm *FileManager) leftPaneWidth() int {
	cutPaths, copyPaths := loadClipboard()
	clipSet := make(map[string]bool)
	for _, p := range cutPaths {
		clipSet[p] = true
	}
	for _, p := range copyPaths {
		clipSet[p] = true
	}

	maxLen := 0
	visible := fm.visibleRows()
	end := fm.offset + visible
	if end > len(fm.entries) {
		end = len(fm.entries)
	}
	for i := fm.offset; i < end; i++ {
		name := fm.entries[i].Name()
		nameLen := utf8.RuneCountInString(name)
		if fm.entries[i].IsDir() {
			nameLen++ // for '/'
		}
		entryPath := fm.selectedEntryPath(fm.entries[i])
		if clipSet[entryPath] {
			nameLen += 2 // indent for clipboard entries
		}
		if nameLen > maxLen {
			maxLen = nameLen
		}
	}
	maxLen += 1 // padding
	if fm.inCloudSyncRoot && !fm.showingWindowsVolumes {
		maxLen += cloudMarkerCols
	}
	if !fm.showingWindowsVolumes {
		maxLen += sizeCols
	}
	maxWidth := fm.cols / 2
	if maxLen > maxWidth {
		maxLen = maxWidth
	}
	if maxLen < 10 {
		maxLen = 10
	}
	return maxLen
}

// showSizeColumn reports whether the left pane has room for the size column.
func (fm *FileManager) showSizeColumn(leftW int, markerW int) bool {
	if fm.showingWindowsVolumes {
		return false
	}
	// 1 for the space before the name, 2 for a clipboard indent.
	return leftW-1-2-markerW-sizeCols >= minNameColsWithSize
}

// entrySize returns the formatted size of a file, or "" for a directory or a
// file whose size cannot be read. Symbolic links show the size of their target.
// On Windows the size comes from the directory listing, so reading it never
// downloads a cloud only file.
func (fm *FileManager) entrySize(entry os.DirEntry) string {
	if entry.IsDir() {
		return ""
	}
	size := fm.entryBytes(entry)
	if size < 0 {
		return ""
	}
	return formatColumnSize(size)
}

// entryBytes returns the size in bytes of a file, or -1 when it cannot be
// read, and caches it in entrySizes.
func (fm *FileManager) entryBytes(entry os.DirEntry) int64 {
	path := fm.selectedEntryPath(entry)
	if size, ok := fm.entrySizes[path]; ok {
		return size
	}
	size := int64(-1)
	var info fs.FileInfo
	var err error
	if entry.Type()&fs.ModeSymlink != 0 {
		info, err = os.Stat(path)
	} else {
		info, err = entry.Info()
	}
	if err == nil && info != nil && !info.IsDir() {
		size = info.Size()
	}
	if fm.entrySizes == nil {
		fm.entrySizes = make(map[string]int64)
	}
	fm.entrySizes[path] = size
	fm.knownSizes++
	if size > 0 {
		fm.totalFileSize += size
	}
	return size
}

// directorySummary describes the current directory for the bottom row, such
// as "3 directories, 12 files, 4.5 MiB". The total size is left off until
// every file's size is known, so showing it never costs extra stat calls.
func (fm *FileManager) directorySummary() string {
	if fm.showingWindowsVolumes {
		return ""
	}
	if fm.dirCount == 0 && fm.fileCount == 0 {
		return "empty directory"
	}
	dirNoun := "directories"
	if fm.dirCount == 1 {
		dirNoun = "directory"
	}
	fileNoun := "files"
	if fm.fileCount == 1 {
		fileNoun = "file"
	}
	summary := fmt.Sprintf("%d %s, %d %s", fm.dirCount, dirNoun, fm.fileCount, fileNoun)
	if fm.fileCount > 0 && fm.knownSizes >= fm.fileCount {
		summary += ", " + formatPreviewSize(fm.totalFileSize)
	}
	return summary
}

// formatColumnSize formats a byte count in at most four columns, like ls -h:
// bytes as a plain number, then K, M, G, T, P in powers of 1024, with one
// decimal below 10.
func formatColumnSize(size int64) string {
	if size < 1024 {
		return strconv.FormatInt(size, 10)
	}
	value := float64(size)
	for _, unit := range []string{"K", "M", "G", "T", "P", "E"} {
		value /= 1024
		// Move to the next unit where rounding would reach four digits.
		if value < 999.5 || unit == "E" {
			if value < 9.95 {
				return fmt.Sprintf("%.1f%s", value, unit)
			}
			return fmt.Sprintf("%.0f%s", value, unit)
		}
	}
	return ""
}

// sizeColor returns the color for a size from formatColumnSize, so each unit
// stands out from the next: bytes are dim, then kilobytes plain, megabytes
// cyan, gigabytes yellow, and anything larger magenta.
func sizeColor(size string) string {
	if size == "" {
		return "\033[39m"
	}
	switch size[len(size)-1] {
	case 'K':
		return "\033[39m" // default
	case 'M':
		return "\033[36m" // cyan
	case 'G':
		return "\033[33m" // yellow
	case 'T', 'P', 'E':
		return "\033[35m" // magenta
	}
	return "\033[90m" // bytes, dim gray
}

// truncateMiddle truncates s to maxRunes by replacing the middle with "..".
func truncateMiddle(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	if maxRunes <= 2 {
		return string(runes[:maxRunes])
	}
	// Split available space: more on the left
	leftLen := (maxRunes - 2 + 1) / 2
	rightLen := maxRunes - 2 - leftLen
	return string(runes[:leftLen]) + ".." + string(runes[len(runes)-rightLen:])
}

func (fm *FileManager) writeHighlightedName(buf *bytes.Buffer, name string, isDir bool, isCursor bool) {
	if !fm.searchActive || len(fm.searchTerm) == 0 {
		buf.WriteString(name)
		return
	}

	nameLower := strings.ToLower(name)
	matchStart := strings.Index(nameLower, fm.searchTerm)
	if matchStart < 0 {
		buf.WriteString(name)
		return
	}

	runes := []rune(name)
	termRunes := utf8.RuneCountInString(fm.searchTerm)

	// Find rune index of match start
	runeIdx := 0
	byteIdx := 0
	for runeIdx < len(runes) && byteIdx < matchStart {
		byteIdx += utf8.RuneLen(runes[runeIdx])
		runeIdx++
	}
	matchRuneStart := runeIdx
	matchRuneEnd := matchRuneStart + termRunes
	if matchRuneEnd > len(runes) {
		matchRuneEnd = len(runes)
	}

	// Write pre-match
	buf.WriteString(string(runes[:matchRuneStart]))
	// Highlight: yellow background
	buf.WriteString("\033[33;1m")
	buf.WriteString(string(runes[matchRuneStart:matchRuneEnd]))
	// Restore previous style
	buf.WriteString("\033[22;39m")
	if isDir {
		buf.WriteString("\033[34m")
	}
	if isCursor {
		buf.WriteString("\033[7m")
	}
	// Write post-match
	buf.WriteString(string(runes[matchRuneEnd:]))
}

func (fm *FileManager) render() {
	var buf bytes.Buffer

	// Hide the cursor, move it to top-left, clear screen
	buf.WriteString("\033[?25l\033[H\033[2J")

	leftW := fm.leftPaneWidth()
	markerW := 0
	if fm.inCloudSyncRoot && !fm.showingWindowsVolumes {
		markerW = cloudMarkerCols
	}
	sizeW := 0
	if fm.showSizeColumn(leftW, markerW) {
		sizeW = sizeCols
	}
	rightW := fm.cols - leftW - 3 // 3 for " │ " separator
	if rightW < 0 {
		rightW = 0
	}

	// Load clipboard for display
	cutPaths, copyPaths := loadClipboard()
	cutSet := make(map[string]bool)
	copySet := make(map[string]bool)
	for _, p := range cutPaths {
		cutSet[p] = true
	}
	for _, p := range copyPaths {
		copySet[p] = true
	}

	// Header row
	headerDir := fm.currentDir
	if fm.showingWindowsVolumes {
		headerDir = "Windows volumes"
	}
	header := fmt.Sprintf(" %s@%s: %s", fm.username, fm.hostname, headerDir)

	// Clipboard status suffix
	clipStatus := ""
	if len(cutPaths) > 0 {
		noun := "file"
		if len(cutPaths) != 1 {
			noun = "files"
		}
		clipStatus = fmt.Sprintf(" CUT %d %s", len(cutPaths), noun)
	}
	if len(copyPaths) > 0 {
		noun := "file"
		if len(copyPaths) != 1 {
			noun = "files"
		}
		clipStatus += fmt.Sprintf(" COPY %d %s", len(copyPaths), noun)
	}
	if len(fm.selected) > 0 {
		clipStatus += fmt.Sprintf(" SELECTED %d", len(fm.selected))
	}

	headerRunes := utf8.RuneCountInString(header)
	clipStatusRunes := utf8.RuneCountInString(clipStatus)

	buf.WriteString("\033[7m") // reverse video
	if headerRunes+clipStatusRunes > fm.cols {
		header = truncateMiddle(header, fm.cols-clipStatusRunes)
		headerRunes = fm.cols - clipStatusRunes
	}
	buf.WriteString(header)
	// Pad between header and clip status
	pad := fm.cols - headerRunes - clipStatusRunes
	if pad > 0 {
		buf.WriteString(strings.Repeat(" ", pad))
	}
	if clipStatus != "" {
		buf.WriteString("\033[36m") // cyan
		buf.WriteString(clipStatus)
		buf.WriteString("\033[39m") // reset fg, keep reverse
	}
	buf.WriteString("\033[0m")

	if fm.mode == fmModeHelp {
		fm.renderHelp(&buf)
		if fm.popup {
			fm.drawOverlayBox(&buf, fm.popupLines())
		}
		fm.ttyOut.Write(buf.Bytes())
		return
	}

	// Preview content
	previewLines := fm.getPreview()

	visible := fm.visibleRows()
	for row := 0; row < visible; row++ {
		buf.WriteString("\r\n")
		idx := fm.offset + row

		// Left pane entry
		if idx < len(fm.entries) {
			entry := fm.entries[idx]
			name := entry.Name()
			if entry.IsDir() {
				name += "/"
			}

			entryPath := fm.selectedEntryPath(entry)
			inCut := cutSet[entryPath]
			inCopy := copySet[entryPath]
			inClip := inCut || inCopy
			indent := 0
			if inClip {
				indent = 2
			}

			nameRunes := utf8.RuneCountInString(name)

			if idx == fm.cursor {
				buf.WriteString("\033[7m") // reverse video for selected
			}

			// In a cloud sync folder, a marker slot shows each entry's sync status.
			if markerW > 0 {
				// Paint the slot first so the selected row's highlight covers it,
				// then draw the marker and jump to the name column. The jump keeps
				// the name aligned however wide the terminal draws the marker.
				buf.WriteString(strings.Repeat(" ", markerW))
				marker, color := cloudStateMarker(fm.cloudState(entry))
				if marker != "" {
					buf.WriteString("\033[2G")
					if idx != fm.cursor {
						buf.WriteString(color)
					}
					buf.WriteString(marker)
					buf.WriteString("\033[39m")
				}
				fmt.Fprintf(&buf, "\033[%dG", 1+markerW)
			}

			if inCut {
				buf.WriteString("\033[31m") // red
			} else if inCopy {
				buf.WriteString("\033[32m") // green
			} else if entry.IsDir() {
				buf.WriteString("\033[34m") // blue for directories
			}

			availW := leftW - 1 - indent - markerW - sizeW // 1 for the space before the name
			if availW < 0 {
				availW = 0
			}
			if nameRunes > availW {
				name = truncateMiddle(name, availW)
				nameRunes = availW
			}
			if fm.selectedSet[entryPath] {
				// A selected entry is marked in the space before its name.
				buf.WriteString("\033[35m*")
				switch {
				case inCut:
					buf.WriteString("\033[31m")
				case inCopy:
					buf.WriteString("\033[32m")
				case entry.IsDir():
					buf.WriteString("\033[34m")
				default:
					buf.WriteString("\033[39m")
				}
			} else {
				buf.WriteString(" ")
			}
			if indent > 0 {
				buf.WriteString(strings.Repeat(" ", indent))
			}
			fm.writeHighlightedName(&buf, name, entry.IsDir(), idx == fm.cursor)
			// Pad to leftW
			padLeft := availW - nameRunes
			if padLeft > 0 {
				buf.WriteString(strings.Repeat(" ", padLeft))
			}

			if sizeW > 0 {
				// Colored by unit on normal rows. The selected row is reverse
				// video, where the name's color is the highlight, so leave it
				// alone to keep the highlight one color across the row.
				size := fm.entrySize(entry)
				if idx != fm.cursor {
					buf.WriteString(sizeColor(size))
				}
				fmt.Fprintf(&buf, "%*s", sizeW, size)
				if idx != fm.cursor {
					buf.WriteString("\033[39m")
				}
			}

			if idx == fm.cursor || inClip {
				buf.WriteString("\033[0m")
			}
		} else {
			buf.WriteString(strings.Repeat(" ", leftW))
		}

		// Separator
		buf.WriteString(" \033[90m\u2502\033[0m ")

		// Right pane
		if row < len(previewLines) {
			line := previewLines[row]
			lineRunes := utf8.RuneCountInString(line)
			if lineRunes > rightW {
				line = truncateMiddle(line, rightW)
			}
			buf.WriteString(line)
		}
	}

	// Image preview, drawn over the empty preview rows. Overlays are text
	// drawn on top of the preview, so the image is left off while one shows.
	if image := fm.selectedImage(); image != nil && !fm.popup && !fm.asking {
		buf.WriteString("\0337") // save cursor position
		fmt.Fprintf(&buf, "\033[%d;%dH", imagePreviewRow, leftW+5)
		buf.Write(image)
		buf.WriteString("\0338") // restore cursor position
	}

	// The bottom: the prompt line, with a message above it, or a message or
	// the directory summary.
	cursorCol := 0
	if fm.mode == fmModeInsert {
		if fm.statusMsg != "" {
			buf.WriteString("\r\n")
			fm.writeStatus(&buf)
		}
		buf.WriteString("\r\n")
		cursorCol = fm.writeLine(&buf)
	} else if fm.statusMsg != "" {
		buf.WriteString("\r\n")
		fm.writeStatus(&buf)
	} else if summary := fm.directorySummary(); summary != "" {
		buf.WriteString("\r\n")
		summary = " " + summary
		if utf8.RuneCountInString(summary) > fm.cols {
			summary = string([]rune(summary)[:fm.cols])
		}
		buf.WriteString("\033[90m") // dim gray
		buf.WriteString(summary)
		buf.WriteString("\033[0m")
	}

	if fm.popup {
		fm.drawOverlayBox(&buf, fm.popupLines())
	}
	if cursorCol > 0 {
		fmt.Fprintf(&buf, "\033[%d;%dH\033[?25h", fm.rows, cursorCol)
	}

	fm.ttyOut.Write(buf.Bytes())
}

// writeStatus writes the status message, in one row.
func (fm *FileManager) writeStatus(buf *bytes.Buffer) {
	msg := terminalSafeText(fm.statusMsg, false)
	if utf8.RuneCountInString(msg) > fm.cols {
		msg = string([]rune(msg)[:fm.cols])
	}
	buf.WriteString("\033[33m") // yellow
	buf.WriteString(msg)
	buf.WriteString("\033[0m")
}

func (fm *FileManager) drawOverlayBox(buf *bytes.Buffer, lines []string) {
	boxW := 0
	for _, l := range lines {
		runes := utf8.RuneCountInString(l)
		if runes > boxW {
			boxW = runes
		}
	}
	boxW += 2
	if boxW > fm.cols-4 {
		boxW = fm.cols - 4
	}

	startCol := (fm.cols - boxW) / 2
	if startCol < 1 {
		startCol = 1
	}
	startRow := (fm.rows-len(lines))/2 + 1
	if startRow < 2 {
		startRow = 2
	}

	for i, line := range lines {
		row := startRow + i
		if row > fm.rows {
			break
		}
		buf.WriteString(fmt.Sprintf("\033[%d;%dH", row, startCol))
		buf.WriteString("\033[7m")
		lineRunes := utf8.RuneCountInString(line)
		if lineRunes > boxW {
			line = truncateMiddle(line, boxW)
			lineRunes = boxW
		}
		buf.WriteString(line)
		pad := boxW - lineRunes
		if pad > 0 {
			buf.WriteString(strings.Repeat(" ", pad))
		}
		buf.WriteString("\033[0m")
	}
}

// bookmarkLines are the lines of the question that sets a bookmark, or
// goes to one.
func (fm *FileManager) bookmarkLines(set bool) []string {
	var lines []string
	if set {
		lines = append(lines, " Set bookmark (0-9, a-z, A-Z): ")
	} else {
		lines = append(lines, " Go to bookmark: ")
	}
	for c := byte('0'); c <= '9'; c++ {
		if dir, ok := fm.bookmarks[c]; ok {
			lines = append(lines, fmt.Sprintf("  %c  %s", c, dir))
		}
	}
	for c := byte('a'); c <= 'z'; c++ {
		if dir, ok := fm.bookmarks[c]; ok {
			lines = append(lines, fmt.Sprintf("  %c  %s", c, dir))
		}
	}
	for c := byte('A'); c <= 'Z'; c++ {
		if dir, ok := fm.bookmarks[c]; ok {
			lines = append(lines, fmt.Sprintf("  %c  %s", c, dir))
		}
	}
	if len(lines) == 1 {
		lines = append(lines, "  (no bookmarks)")
	}
	return lines
}

func (fm *FileManager) getPreview() []string {
	if len(fm.entries) == 0 || fm.cursor >= len(fm.entries) {
		return nil
	}

	entry := fm.entries[fm.cursor]
	path := fm.selectedEntryPath(entry)

	if cached, ok := fm.previewCache[path]; ok {
		return cached
	}

	return []string{" Loading..."}
}

func computePreview(entry os.DirEntry, path string, maxLines int, onlineOnly bool) []string {
	// Reading a cloud only file would download it, which can take a long time
	// and fills the disk with files the user was only scrolling past.
	if onlineOnly {
		return []string{" (cloud only, not downloaded)"}
	}

	if entry.IsDir() {
		subEntries, err := os.ReadDir(path)
		if err != nil {
			return []string{" (cannot read)"}
		}
		subEntries = filterIgnoredFileManagerEntries(subEntries)
		sort.SliceStable(subEntries, func(i, j int) bool {
			iDir := subEntries[i].IsDir()
			jDir := subEntries[j].IsDir()
			if iDir != jDir {
				return iDir
			}
			return VersionSortComparer(strings.ToLower(subEntries[i].Name()), strings.ToLower(subEntries[j].Name())) < 0
		})
		var lines []string
		for i, e := range subEntries {
			if i >= maxLines {
				break
			}
			name := e.Name()
			if e.IsDir() {
				lines = append(lines, " \033[34m"+name+"/\033[0m")
			} else {
				lines = append(lines, " "+name)
			}
		}
		return lines
	}

	// Only regular files: a named pipe or device, such as a link to
	// /dev/zero, could block or never end.
	if stat, err := os.Stat(path); err != nil {
		return []string{" (cannot open for reading)"}
	} else if !stat.Mode().IsRegular() {
		return []string{" (not a regular file)"}
	}

	if isZipPreviewPath(path) {
		return previewZipArchive(path, maxLines)
	}

	if isTarGzPreviewPath(path) {
		return previewTarGzArchive(path, maxLines)
	}

	if isImagePreviewPath(path) {
		lines, _ := previewImage(path, 0, 0, 1)
		return lines
	}

	if isPdfPreviewPath(path) {
		return previewPdf(path, maxLines)
	}

	if hasKnownBinaryPreviewExtension(path) {
		return []string{" (binary file)"}
	}

	// Check for binary before reading the file
	f, err := os.Open(path)
	if err != nil {
		return []string{" (cannot open for reading)"}
	}
	defer f.Close()

	probe := make([]byte, 512)
	n, _ := f.Read(probe)
	if n > 0 && bytes.ContainsRune(probe[:n], 0) {
		return []string{" (binary file)"}
	}

	// Read the start of the file for the text preview. The pane shows at
	// most a screen of lines, so there is no need to read more.
	f.Seek(0, 0)
	data, err := io.ReadAll(io.LimitReader(f, maxTextPreviewBytes))
	if err != nil {
		return []string{" (cannot read)"}
	}

	allLines := strings.Split(string(data), "\n")
	var lines []string
	for i, line := range allLines {
		if i >= maxLines {
			break
		}
		// Replace tabs with spaces for display
		line = strings.ReplaceAll(line, "\t", "    ")
		lines = append(lines, " "+line)
	}
	return lines
}

// maxTextPreviewBytes is the most read from a file for its text preview.
const maxTextPreviewBytes = 1 << 20

func filterIgnoredFileManagerEntries(entries []os.DirEntry) []os.DirEntry {
	filtered := make([]os.DirEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Name() == oneDriveHiddenMetadataFileName {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func isZipPreviewPath(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".zip")
}

func isTarGzPreviewPath(path string) bool {
	lowerPath := strings.ToLower(path)
	return strings.HasSuffix(lowerPath, ".tar.gz") || strings.HasSuffix(lowerPath, ".tgz")
}

func hasKnownBinaryPreviewExtension(path string) bool {
	lowerPath := strings.ToLower(path)
	for _, suffix := range []string{
		".aac",
		".ai",
		".apk",
		".appimage",
		".avi",
		".bmp",
		".deb",
		".dll",
		".doc",
		".docx",
		".dylib",
		".exe",
		".flac",
		".gif",
		".heic",
		".ico",
		".img",
		".iso",
		".jar",
		".jpeg",
		".jpg",
		".m4a",
		".mkv",
		".mov",
		".mp3",
		".mp4",
		".msi",
		".ogg",
		".otf",
		".pdf",
		".png",
		".ppt",
		".pptx",
		".rpm",
		".so",
		".tif",
		".tiff",
		".ttf",
		".vhd",
		".vhdx",
		".wav",
		".webm",
		".webp",
		".wmv",
		".woff",
		".woff2",
		".xls",
		".xlsb",
		".xlsm",
		".xlsx",
	} {
		if strings.HasSuffix(lowerPath, suffix) {
			return true
		}
	}
	return false
}

func previewZipArchive(path string, maxLines int) []string {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return []string{" (cannot read zip archive)"}
	}
	defer reader.Close()

	entries := make([]archiveListingEntry, 0, len(reader.File))
	var totalSize int64
	fileCount := 0
	dirCount := 0
	for _, file := range reader.File {
		entry := archiveListingEntry{
			name:     file.Name,
			isDir:    file.FileInfo().IsDir(),
			size:     int64(file.UncompressedSize64),
			modified: file.Modified,
		}
		entries = append(entries, entry)
		if entry.isDir {
			dirCount++
		} else {
			fileCount++
			totalSize += entry.size
		}
	}

	return buildDetailedArchivePreviewLines(path, entries, totalSize, fileCount, dirCount, maxLines)
}

func buildDetailedArchivePreviewLines(path string, entries []archiveListingEntry, totalSize int64, fileCount int, dirCount int, maxLines int) []string {
	sizeWidth := len("Length")
	for _, entry := range entries {
		sizeLen := len(formatPreviewSize(entry.size))
		if sizeLen > sizeWidth {
			sizeWidth = sizeLen
		}
	}

	headerLine := fmt.Sprintf(" %*s  %-10s %-8s  %s", sizeWidth, "Length", "Date", "Time", "Name")
	dashLine := fmt.Sprintf(" %s  %s %s  %s",
		strings.Repeat("-", sizeWidth),
		strings.Repeat("-", 10),
		strings.Repeat("-", 8),
		strings.Repeat("-", 4),
	)

	lines := []string{
		fmt.Sprintf(" Archive:  %s", path),
		headerLine,
		dashLine,
	}

	for _, entry := range entries {
		name := entry.name
		if entry.isDir && !strings.HasSuffix(name, "/") {
			name += "/"
		}

		dateStr := "----------"
		timeStr := "--------"
		if !entry.modified.IsZero() {
			dateStr = entry.modified.Format("2006-01-02")
			timeStr = entry.modified.Format("3:04 PM")
		}

		lines = append(lines, fmt.Sprintf(
			" %*s  %s %-8s  %s",
			sizeWidth,
			formatPreviewSize(entry.size),
			dateStr,
			timeStr,
			name,
		))
	}

	summaryLabel := fmt.Sprintf("%d files", fileCount)
	if dirCount > 0 {
		summaryLabel = fmt.Sprintf("%s, %d dirs", summaryLabel, dirCount)
	}
	lines = append(lines, dashLine)
	lines = append(lines, fmt.Sprintf(" %*s  %s %s  %s",
		sizeWidth,
		formatPreviewSize(totalSize),
		strings.Repeat(" ", 10),
		strings.Repeat(" ", 8),
		summaryLabel,
	))

	return truncatePreviewMiddle(lines, 3, 2, maxLines)
}

func truncatePreviewMiddle(lines []string, keepHead int, keepTail int, maxLines int) []string {
	if maxLines <= 0 || len(lines) <= maxLines {
		return lines
	}
	if maxLines == 1 {
		return lines[:1]
	}
	if keepHead+keepTail >= maxLines {
		return truncatePreviewLines(lines, maxLines)
	}

	head := append([]string{}, lines[:keepHead]...)
	tail := append([]string{}, lines[len(lines)-keepTail:]...)
	omitted := len(lines) - len(head) - len(tail)
	head = append(head, fmt.Sprintf(" ... %d more", omitted))
	head = append(head, tail...)
	return head
}

func previewTarGzArchive(path string, maxLines int) []string {
	file, err := os.Open(path)
	if err != nil {
		return []string{" (cannot open archive)"}
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return []string{" (cannot read tar.gz archive)"}
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	entries := make([]archiveListingEntry, 0)
	var totalSize int64
	fileCount := 0
	dirCount := 0
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return []string{" (cannot read tar.gz archive)"}
		}

		entry := archiveListingEntry{
			name:  header.Name,
			isDir: header.FileInfo().IsDir(),
			size:  header.Size,
			modified: header.ModTime,
		}
		entries = append(entries, entry)
		if entry.isDir {
			dirCount++
		} else {
			fileCount++
			totalSize += entry.size
		}
	}

	return buildDetailedArchivePreviewLines(path, entries, totalSize, fileCount, dirCount, maxLines)
}

func truncatePreviewLines(lines []string, maxLines int) []string {
	if maxLines <= 0 || len(lines) <= maxLines {
		return lines
	}
	if maxLines == 1 {
		return lines[:1]
	}

	remaining := len(lines) - maxLines + 1
	truncated := append([]string{}, lines[:maxLines-1]...)
	truncated = append(truncated, fmt.Sprintf(" ... %d more", remaining))
	return truncated
}

func formatPreviewSize(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}

	value := float64(size)
	for _, unit := range []string{"KiB", "MiB", "GiB", "TiB"} {
		value /= 1024
		if value < 1024 {
			return fmt.Sprintf("%.1f %s", value, unit)
		}
	}
	return fmt.Sprintf("%.1f PiB", value/1024)
}

// updateSearchLive follows the search line as it changes: the matches are
// those of the query.
func (fm *FileManager) updateSearchLive(query string) {
	if query == "" {
		fm.searchMatches = fm.searchMatches[:0]
		fm.searchActive = false
		return
	}
	fm.searchTerm = strings.ToLower(query)
	fm.buildSearchMatches()
	fm.searchActive = true
}

// commitSearch puts the cursor on the first match of the query at or
// after it.
func (fm *FileManager) commitSearch(query string) {
	if query == "" {
		fm.searchActive = false
		fm.searchMatches = fm.searchMatches[:0]
		return
	}
	fm.searchTerm = strings.ToLower(query)
	fm.searchActive = true
	fm.buildSearchMatches()
	if len(fm.searchMatches) > 0 {
		// Jump to first match at or after cursor
		for _, idx := range fm.searchMatches {
			if idx >= fm.cursor {
				fm.cursor = idx
				fm.adjustScroll()
				return
			}
		}
		fm.cursor = fm.searchMatches[0]
		fm.adjustScroll()
	}
}

func (fm *FileManager) buildSearchMatches() {
	fm.searchMatches = fm.searchMatches[:0]
	for i, e := range fm.entries {
		if strings.Contains(strings.ToLower(e.Name()), fm.searchTerm) {
			fm.searchMatches = append(fm.searchMatches, i)
		}
	}
}

func (fm *FileManager) searchNext() {
	if !fm.searchActive || len(fm.searchMatches) == 0 {
		return
	}
	for _, idx := range fm.searchMatches {
		if idx > fm.cursor {
			fm.cursor = idx
			fm.adjustScroll()
			return
		}
	}
	// Wrap around
	fm.cursor = fm.searchMatches[0]
	fm.adjustScroll()
}

func (fm *FileManager) searchPrev() {
	if !fm.searchActive || len(fm.searchMatches) == 0 {
		return
	}
	for i := len(fm.searchMatches) - 1; i >= 0; i-- {
		if fm.searchMatches[i] < fm.cursor {
			fm.cursor = fm.searchMatches[i]
			fm.adjustScroll()
			return
		}
	}
	// Wrap around
	fm.cursor = fm.searchMatches[len(fm.searchMatches)-1]
	fm.adjustScroll()
}

// commitRename renames the current entry, and puts the cursor on it.
func (fm *FileManager) commitRename(newName string) {
	if len(fm.entries) == 0 || fm.cursor >= len(fm.entries) {
		return
	}
	oldName := fm.entries[fm.cursor].Name()
	if newName == "" || newName == oldName {
		return
	}

	oldPath := filepath.Join(fm.currentDir, oldName)
	newPath := filepath.Join(fm.currentDir, newName)
	err := os.Rename(oldPath, newPath)
	if err != nil {
		fm.statusMsg = "Rename: " + err.Error()
		return
	}

	fm.loadDirectory()
	// Try to select the renamed entry
	for i, e := range fm.entries {
		if e.Name() == newName {
			fm.cursor = i
			fm.adjustScroll()
			break
		}
	}
}

func (fm *FileManager) enterSelected() {
	if len(fm.entries) == 0 || fm.cursor >= len(fm.entries) {
		return
	}
	entry := fm.entries[fm.cursor]
	if fm.showingWindowsVolumes {
		fm.currentDir = windowsVolumeRoot(entry.Name())
		fm.cursor = 0
		fm.offset = 0
		fm.loadDirectory()
		return
	}
	if !entry.IsDir() {
		switch runtime.GOOS {
		case "windows":
			fm.openFileWindows(entry)
		case "linux", "darwin":
			fm.openFileUnix(entry)
		}
		return
	}
	newDir := filepath.Join(fm.currentDir, entry.Name())
	fm.currentDir = newDir
	fm.cursor = 0
	fm.offset = 0
	fm.loadDirectory()
}

func (fm *FileManager) goParent() {
	if fm.showingWindowsVolumes {
		return
	}
	if isWindowsVolumeRoot(fm.currentDir) {
		fm.showWindowsVolumes()
		return
	}
	parent := filepath.Dir(fm.currentDir)
	if parent == fm.currentDir {
		return
	}
	oldName := filepath.Base(fm.currentDir)
	fm.currentDir = parent
	fm.cursor = 0
	fm.offset = 0
	fm.loadDirectory()

	// Try to restore cursor to previous directory
	for i, e := range fm.entries {
		if e.Name() == oldName {
			fm.cursor = i
			fm.adjustScroll()
			break
		}
	}
}

// openEditor opens the selected files, or the current one, in $EDITOR.
// Directories are left out.
func (fm *FileManager) openEditor() {
	paths, missing := fm.targets()
	var files []string
	for _, p := range paths {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			files = append(files, p)
		}
	}
	fm.noteMissing(missing)
	if len(files) == 0 {
		return
	}

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	fm.runWithTerminal(func() {
		if err := fm.runEditor(editor, files...); err != nil {
			fmt.Fprintf(os.Stderr, "editor %q exited with error: %v\n", editor, err)
			waitForEnter()
		}
	})
}

// runEditor runs the editor on files, in the directory shown, with the
// terminal as its input and output.
func (fm *FileManager) runEditor(editor string, files ...string) error {
	cmd := exec.Command(editor, files...)
	cmd.Dir = fm.currentDir
	cmd.Stdin = os.Stdin
	cmd.Stdout = fm.ttyOut
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func isBinaryFile(path string) bool {
	if hasKnownBinaryPreviewExtension(path) || isZipPreviewPath(path) || isTarGzPreviewPath(path) {
		return true
	}
	f, err := os.Open(path)
	if err != nil {
		return true
	}
	defer f.Close()
	probe := make([]byte, 512)
	n, _ := f.Read(probe)
	return n > 0 && bytes.ContainsRune(probe[:n], 0)
}

func (fm *FileManager) openFileWindows(entry os.DirEntry) {
	filePath := filepath.Join(fm.currentDir, entry.Name())

	editor := os.Getenv("EDITOR")
	if editor != "" && !isBinaryFile(filePath) {
		// Open text file in editor, same as 'e' binding
		var err error
		fm.runWithTerminal(func() { err = fm.runEditor(editor, filePath) })

		if err == nil {
			return
		}
		// Editor failed; fall through to Start-Process
	}

	// Binary file, no $EDITOR, or editor failed: open with Windows default app
	command, args, err := defaultAppCommand(filePath, runtime.GOOS)
	if err != nil {
		fm.statusMsg = err.Error()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Stdin = nil
	if err := cmd.Run(); err != nil && ctx.Err() == context.DeadlineExceeded {
		fm.statusMsg = "Start-Process timed out (OneDrive?)"
	}
}

func (fm *FileManager) openFileUnix(entry os.DirEntry) {
	filePath := filepath.Join(fm.currentDir, entry.Name())

	editor := os.Getenv("EDITOR")
	if editor != "" && !isBinaryFile(filePath) {
		// Open text file in editor, same as 'e' binding
		var err error
		fm.runWithTerminal(func() { err = fm.runEditor(editor, filePath) })

		if err == nil {
			return
		}
		// Editor failed; fall through to xdg-open/open
	}

	// Binary file, no $EDITOR, or editor failed: open with system default
	command, args, err := defaultAppCommand(filePath, runtime.GOOS)
	if err != nil {
		fm.statusMsg = err.Error()
		return
	}
	cmd := exec.Command(command, args...)
	cmd.Stdin = nil
	cmd.Start()
}

// Clipboard actions

type yankKind int

const (
	yankFileName yankKind = iota
	yankFullPath
	yankGitPath
)

// yankSystemClipboard puts the names or paths of the selected entries, or
// of the current one, on the system clipboard, one per line.
func (fm *FileManager) yankSystemClipboard(kind yankKind) {
	paths, missing := fm.targets()
	defer fm.noteMissing(missing)
	if len(paths) == 0 {
		return
	}

	texts := make([]string, len(paths))
	var label string
	for i, absPath := range paths {
		switch kind {
		case yankFileName:
			texts[i] = filepath.Base(absPath)
			label = "name"
		case yankFullPath:
			texts[i] = absPath
			label = "path"
		case yankGitPath:
			rel, err := gitRelativePath(absPath)
			if err != nil {
				fm.statusMsg = err.Error()
				return
			}
			texts[i] = rel
			label = "git path"
		}
	}
	text := strings.Join(texts, "\n")

	if err := writeSystemClipboard(text); err != nil {
		fm.statusMsg = fmt.Sprintf("Clipboard: %s", err.Error())
		return
	}
	if len(texts) == 1 {
		fm.statusMsg = fmt.Sprintf("Copied %s: %s", label, text)
	} else {
		fm.statusMsg = fmt.Sprintf("Copied %d %ss", len(texts), label)
	}
}

func gitRelativePath(absPath string) (string, error) {
	dir := absPath
	if info, err := os.Stat(absPath); err != nil || !info.IsDir() {
		dir = filepath.Dir(absPath)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			rel, err := filepath.Rel(dir, absPath)
			if err != nil {
				return "", err
			}
			return filepath.ToSlash(rel), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no .git directory found above %s", absPath)
		}
		dir = parent
	}
}

func (fm *FileManager) clipboardPaste() {
	cutPaths, copyPaths := loadClipboard()
	if len(cutPaths) == 0 && len(copyPaths) == 0 {
		return
	}

	if !fm.pasteClipboardPaths("cut", cutPaths) {
		return
	}
	if !fm.pasteClipboardPaths("copy", copyPaths) {
		return
	}

	if len(cutPaths) > 0 {
		_ = saveClipboard(nil, copyPaths)
	}
	fm.loadDirectory()
	fm.clampCursor()
	fm.adjustScroll()
}

func (fm *FileManager) pasteClipboardPaths(op string, paths []string) bool {
	for _, src := range paths {
		base := filepath.Base(src)
		dest := filepath.Join(fm.currentDir, base)
		if src == dest {
			if op == "cut" {
				continue
			}
			// Copy to same directory: go straight to versioned name
			dest = versionedPath(dest)
		} else if _, err := os.Lstat(dest); err == nil {
			// Destination exists but is a different file
			choice := fm.promptConflict(base)
			if choice == 0 {
				// Cancel
				return false
			}
			if choice == 2 {
				// Version number
				dest = versionedPath(dest)
			}
			// choice == 1: overwrite, use dest as-is
		}

		if op == "cut" {
			if err := os.Rename(src, dest); err != nil {
				return false
			}
		} else {
			if err := CopyFile(src, dest); err != nil {
				return false
			}
		}
	}
	return true
}

// promptConflict asks the user how to handle a name conflict.
// Returns 0=cancel, 1=overwrite, 2=version number.
func (fm *FileManager) promptConflict(name string) int {
	key, ok := fm.ask([]string{
		fmt.Sprintf(" \"%s\" already exists ", name),
		"",
		"  o  Overwrite",
		"  v  Rename with version number",
		"  Esc  Cancel",
	})
	if !ok {
		return 0
	}
	switch answerRune(key) {
	case 'o':
		return 1
	case 'v':
		return 2
	default:
		return 0
	}
}

// versionedPath returns a path with a version number appended, e.g.
// "file.txt" -> "file (1).txt", "dir" -> "dir (1)".
func versionedPath(dest string) string {
	dir := filepath.Dir(dest)
	base := filepath.Base(dest)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)

	for i := 1; ; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
		if _, err := os.Lstat(candidate); err != nil {
			return candidate
		}
	}
}

// Delete to trash

// deleteEntry moves the selected entries, or the current one, to the
// trash, after a question.
func (fm *FileManager) deleteEntry() {
	paths, missing := fm.targets()
	defer fm.noteMissing(missing)
	if len(paths) == 0 || !fm.promptDelete(paths) {
		return
	}
	for _, p := range paths {
		if err := TrashFile(p); err != nil {
			fm.statusMsg = fmt.Sprintf("Trash: %s", err.Error())
			break
		}
	}
	fm.loadDirectory()
	fm.clampCursor()
	fm.adjustScroll()
}

// promptDelete asks before entries go to the trash. Returns true if the
// user confirms.
func (fm *FileManager) promptDelete(paths []string) bool {
	var lines []string
	if len(paths) == 1 {
		lines = append(lines, fmt.Sprintf(" Delete \"%s\"? ", filepath.Base(paths[0])))
		// For non-empty directories, show entry count
		if subEntries, err := os.ReadDir(paths[0]); err == nil {
			subEntries = filterIgnoredFileManagerEntries(subEntries)
			if len(subEntries) > 0 {
				lines = append(lines, fmt.Sprintf(" (directory with %d entries) ", len(subEntries)))
			}
		}
	} else {
		lines = append(lines, fmt.Sprintf(" Delete %d entries? ", len(paths)))
		// As many names as fit, with the question's other rows.
		shown := min(len(paths), max(1, fm.rows-8))
		for _, p := range paths[:shown] {
			lines = append(lines, "  "+filepath.Base(p))
		}
		if shown < len(paths) {
			lines = append(lines, fmt.Sprintf("  ... and %d more", len(paths)-shown))
		}
	}

	lines = append(lines, "")
	lines = append(lines, "  y  Confirm")
	lines = append(lines, "  Esc  Cancel")

	key, ok := fm.ask(lines)
	return ok && answerRune(key) == 'y'
}

// Bookmarks

const bookmarksFileName = "fm_bookmarks"

func bookmarksFilePath() (string, error) {
	dir, err := GetHistoryDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, bookmarksFileName), nil
}

func loadBookmarks() map[byte]string {
	bookmarks := make(map[byte]string)
	path, err := bookmarksFilePath()
	if err != nil {
		return bookmarks
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return bookmarks
	}
	for _, line := range strings.Split(string(data), "\n") {
		if len(line) >= 3 && line[1] == ' ' {
			bookmarks[line[0]] = line[2:]
		}
	}
	return bookmarks
}

func isBookmarkChar(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// Clipboard

const clipboardFileName = "fm_clipboard"

func clipboardFilePath() (string, error) {
	dir, err := GetHistoryDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, clipboardFileName), nil
}

func loadClipboard() ([]string, []string) {
	path, err := clipboardFilePath()
	if err != nil {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	trimmed := strings.TrimRight(string(data), "\n")
	if trimmed == "" {
		return nil, nil
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) == 0 {
		return nil, nil
	}

	var cutPaths []string
	var copyPaths []string
	dirty := false
	for _, line := range lines {
		if line == "" {
			dirty = true
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			dirty = true
			continue
		}
		op := parts[0]
		path := parts[1]
		if path == "" {
			dirty = true
			continue
		}
		if op == "cut" {
			copyPaths = removePath(copyPaths, path)
			cutPaths = appendUniquePath(cutPaths, path)
		} else if op == "copy" {
			cutPaths = removePath(cutPaths, path)
			copyPaths = appendUniquePath(copyPaths, path)
		} else {
			dirty = true
		}
	}
	if dirty {
		_ = saveClipboard(cutPaths, copyPaths)
	}
	return cutPaths, copyPaths
}

func saveClipboard(cutPaths []string, copyPaths []string) error {
	if len(cutPaths) == 0 && len(copyPaths) == 0 {
		return clearClipboard()
	}
	path, err := clipboardFilePath()
	if err != nil {
		return err
	}
	var sb strings.Builder
	for _, p := range cutPaths {
		sb.WriteString("cut\t")
		sb.WriteString(p)
		sb.WriteByte('\n')
	}
	for _, p := range copyPaths {
		sb.WriteString("copy\t")
		sb.WriteString(p)
		sb.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(sb.String()), 0644)
}

func clearClipboard() error {
	path, err := clipboardFilePath()
	if err != nil {
		return err
	}
	return os.Remove(path)
}

func appendUniquePath(paths []string, path string) []string {
	if slices.Contains(paths, path) {
			return paths
		}
	return append(paths, path)
}

func removePath(paths []string, path string) []string {
	for i, existing := range paths {
		if existing == path {
			return append(paths[:i], paths[i+1:]...)
		}
	}
	return paths
}

func addClipboardPath(op string, path string) error {
	cutPaths, copyPaths := loadClipboard()
	if op == "cut" {
		copyPaths = removePath(copyPaths, path)
		cutPaths = appendUniquePath(cutPaths, path)
	} else if op == "copy" {
		cutPaths = removePath(cutPaths, path)
		copyPaths = appendUniquePath(copyPaths, path)
	}
	return saveClipboard(cutPaths, copyPaths)
}

func saveBookmarks(bookmarks map[byte]string) error {
	path, err := bookmarksFilePath()
	if err != nil {
		return err
	}
	var sb strings.Builder
	for c := byte('0'); c <= '9'; c++ {
		if dir, ok := bookmarks[c]; ok {
			sb.WriteByte(c)
			sb.WriteByte(' ')
			sb.WriteString(dir)
			sb.WriteByte('\n')
		}
	}
	for c := byte('a'); c <= 'z'; c++ {
		if dir, ok := bookmarks[c]; ok {
			sb.WriteByte(c)
			sb.WriteByte(' ')
			sb.WriteString(dir)
			sb.WriteByte('\n')
		}
	}
	for c := byte('A'); c <= 'Z'; c++ {
		if dir, ok := bookmarks[c]; ok {
			sb.WriteByte(c)
			sb.WriteByte(' ')
			sb.WriteString(dir)
			sb.WriteByte('\n')
		}
	}
	return os.WriteFile(path, []byte(sb.String()), 0644)
}
