package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// tarEntryMetadata mirrors zipEntryMetadata but carries the tar-specific
// entry type and symlink target. tar has no per-entry compressed size, so
// tarList reports compressedSize == uncompressedSize (documented).
type tarEntryMetadata struct {
	Name       string
	Size       int
	Modified   time.Time
	IsDir      bool
	Mode       os.FileMode
	Type       string // "file", "dir", or "symlink"
	LinkTarget string
}

// funcCloser adapts a plain function to io.Closer so the gzip + file layers
// can be torn down together.
type funcCloser func() error

func (f funcCloser) Close() error { return f() }

// isGzipTarget reports whether a destination path should be gzip-compressed
// based on its extension (.gz or .tgz). Matches `tar -a` auto-compression.
func isGzipTarget(tarPath string) bool {
	lower := strings.ToLower(tarPath)
	return strings.HasSuffix(lower, ".gz") || strings.HasSuffix(lower, ".tgz")
}

// openTarReader opens a tar archive for reading, transparently decompressing
// gzip streams by sniffing the magic bytes (0x1f 0x8b) regardless of the file
// extension. The returned Closer tears down every layer that was opened.
func openTarReader(tarPath string) (*tar.Reader, io.Closer, error) {
	file, err := os.Open(tarPath)
	if err != nil {
		return nil, nil, fmt.Errorf("Error opening %s: %w", tarPath, err)
	}

	br := bufio.NewReader(file)
	magic, err := br.Peek(2)
	if err != nil && err != io.EOF {
		file.Close()
		return nil, nil, fmt.Errorf("Error reading %s: %w", tarPath, err)
	}

	if len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		gz, err := gzip.NewReader(br)
		if err != nil {
			file.Close()
			return nil, nil, fmt.Errorf("Error reading gzip stream %s: %w", tarPath, err)
		}
		closer := funcCloser(func() error {
			gz.Close()
			return file.Close()
		})
		return tar.NewReader(gz), closer, nil
	}

	return tar.NewReader(br), file, nil
}

// createTarWriter creates a tar archive for writing, gzip-compressing when
// compress is true. The returned finish function must be called (not deferred
// with a discarded error) to flush and close every layer.
func createTarWriter(tarPath string, compress bool) (*tar.Writer, func() error, error) {
	if err := os.MkdirAll(filepath.Dir(tarPath), 0755); err != nil {
		return nil, nil, fmt.Errorf("Error creating parent directory for %s: %w", tarPath, err)
	}

	output, err := os.Create(tarPath)
	if err != nil {
		return nil, nil, fmt.Errorf("Error creating %s: %w", tarPath, err)
	}

	if compress {
		gz := gzip.NewWriter(output)
		tw := tar.NewWriter(gz)
		finish := func() error {
			if err := tw.Close(); err != nil {
				output.Close()
				return fmt.Errorf("Error finalizing tar %s: %w", tarPath, err)
			}
			if err := gz.Close(); err != nil {
				output.Close()
				return fmt.Errorf("Error finalizing gzip %s: %w", tarPath, err)
			}
			return output.Close()
		}
		return tw, finish, nil
	}

	tw := tar.NewWriter(output)
	finish := func() error {
		if err := tw.Close(); err != nil {
			output.Close()
			return fmt.Errorf("Error finalizing tar %s: %w", tarPath, err)
		}
		return output.Close()
	}
	return tw, finish, nil
}

// tarDirectory packs a single directory into a tarball, mirroring zipDirectory.
// preserveRoot controls whether the directory itself appears at the archive
// root (tarDirExc) or only its contents (tarDirInc).
func tarDirectory(sourceDir, tarPath string, preserveRoot bool, compress bool) error {
	info, err := os.Stat(sourceDir)
	if err != nil {
		return fmt.Errorf("Error stating %s: %w", sourceDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("tarDir expects a directory. %s is not a directory", sourceDir)
	}

	srcAbs, err := filepath.Abs(sourceDir)
	if err != nil {
		return fmt.Errorf("Error resolving %s: %w", sourceDir, err)
	}
	if err := ensureTarTargetNotInsideSource(srcAbs, tarPath); err != nil {
		return err
	}

	packItem := zipPackItem{
		SourcePath:   sourceDir,
		PreserveRoot: preserveRoot,
	}
	return buildTarFromEntries([]zipPackItem{packItem}, tarPath, compress)
}

func ensureTarTargetNotInsideSource(sourceAbs string, tarPath string) error {
	tarAbs, err := filepath.Abs(tarPath)
	if err != nil {
		return fmt.Errorf("Error resolving destination %s: %w", tarPath, err)
	}
	sourceWithSep := ensureTrailingSeparator(sourceAbs)
	if tarAbs == sourceAbs || strings.HasPrefix(tarAbs, sourceWithSep) {
		return fmt.Errorf("Tar destination %s cannot be inside the source directory %s", tarPath, sourceAbs)
	}
	return nil
}

// buildTarFromEntries mirrors buildZipFromEntries, reusing the zipPackItem
// model so the tarPack dispatch parsing is identical to zipPack.
func buildTarFromEntries(items []zipPackItem, tarPath string, compress bool) error {
	if len(items) == 0 {
		return fmt.Errorf("tarPack requires at least one entry")
	}

	tarAbs, err := filepath.Abs(tarPath)
	if err != nil {
		return fmt.Errorf("Error resolving %s: %w", tarPath, err)
	}

	tw, finish, err := createTarWriter(tarPath, compress)
	if err != nil {
		return err
	}

	for _, item := range items {
		info, err := os.Lstat(item.SourcePath)
		if err != nil {
			finish()
			return fmt.Errorf("Error stating %s: %w", item.SourcePath, err)
		}

		sourceAbs, err := filepath.Abs(item.SourcePath)
		if err != nil {
			finish()
			return fmt.Errorf("Error resolving %s: %w", item.SourcePath, err)
		}
		sourceAbsWithSep := ensureTrailingSeparator(sourceAbs)
		if tarAbs == sourceAbs || strings.HasPrefix(tarAbs, sourceAbsWithSep) {
			finish()
			return fmt.Errorf("Tar destination %s cannot be inside the source path %s", tarPath, sourceAbs)
		}

		if info.IsDir() {
			prefix := strings.Trim(item.ArchivePath, "/")
			if prefix == "" && item.PreserveRoot {
				prefix = filepath.Base(sourceAbs)
			}
			if err := addDirectoryToTar(tw, item.SourcePath, prefix, item.ModeOverride); err != nil {
				finish()
				return err
			}
			continue
		}

		name := item.ArchivePath
		if name == "" {
			name = filepath.Base(item.SourcePath)
		}
		name = strings.Trim(name, "/")
		if name == "" {
			finish()
			return fmt.Errorf("tarPack entry for %s produced an empty archive path", item.SourcePath)
		}

		if err := addFileToTar(tw, item.SourcePath, name, info, item.ModeOverride); err != nil {
			finish()
			return err
		}
	}

	if err := finish(); err != nil {
		return err
	}
	return nil
}

func addDirectoryToTar(tw *tar.Writer, sourcePath, archivePrefix string, modeOverride *os.FileMode) error {
	cleanPrefix := strings.Trim(archivePrefix, "/")
	return filepath.WalkDir(sourcePath, func(pathStr string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(sourcePath, pathStr)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)

		var entryName string
		if relPath == "." {
			entryName = cleanPrefix
		} else if cleanPrefix == "" {
			entryName = relPath
		} else {
			entryName = path.Join(cleanPrefix, relPath)
		}

		entryName = strings.Trim(entryName, "/")
		if entryName == "" {
			// Skip the implicit root when no prefix is requested.
			return nil
		}
		if info.IsDir() {
			entryName += "/"
		}

		return addFileToTar(tw, pathStr, entryName, info, modeOverride)
	})
}

// addFileToTar writes a single filesystem entry into the tar stream.
// Regular files, directories, and symlinks are supported; symlinks are stored
// as symlinks (preserving the target) rather than being dereferenced.
func addFileToTar(tw *tar.Writer, sourcePath, entryName string, info os.FileInfo, modeOverride *os.FileMode) error {
	// Reject anything that is not a regular file, directory, or symlink before
	// writing a header, so we never emit a partial entry and never os.Open a
	// fifo/device/socket (which could block indefinitely).
	isSymlink := info.Mode()&os.ModeSymlink != 0
	if !info.IsDir() && !info.Mode().IsRegular() && !isSymlink {
		return fmt.Errorf("tarPack cannot archive %s: unsupported file type", sourcePath)
	}

	linkTarget := ""
	if isSymlink {
		target, err := os.Readlink(sourcePath)
		if err != nil {
			return fmt.Errorf("Error reading symlink %s: %w", sourcePath, err)
		}
		linkTarget = target
	}

	header, err := tar.FileInfoHeader(info, linkTarget)
	if err != nil {
		return err
	}
	header.Name = path.Clean(strings.ReplaceAll(entryName, "\\", "/"))
	if info.IsDir() && !strings.HasSuffix(header.Name, "/") {
		header.Name += "/"
	}
	if modeOverride != nil {
		header.Mode = int64(modeOverride.Perm())
	}

	if err := tw.WriteHeader(header); err != nil {
		return err
	}

	if isSymlink || info.IsDir() {
		return nil
	}

	file, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer file.Close()

	if _, err := io.Copy(tw, file); err != nil {
		return err
	}
	return nil
}

func tarEntryType(header *tar.Header) string {
	switch header.Typeflag {
	case tar.TypeDir:
		return "dir"
	case tar.TypeSymlink:
		return "symlink"
	default:
		return "file"
	}
}

func collectTarMetadata(tarPath string) ([]tarEntryMetadata, error) {
	reader, closer, err := openTarReader(tarPath)
	if err != nil {
		return nil, err
	}
	defer closer.Close()

	entries := make([]tarEntryMetadata, 0)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("Error reading %s: %w", tarPath, err)
		}

		size, err := safeSizeToInt(uint64(header.Size))
		if err != nil {
			return nil, err
		}

		info := header.FileInfo()
		entries = append(entries, tarEntryMetadata{
			Name:       header.Name,
			Size:       size,
			Modified:   header.ModTime,
			IsDir:      info.IsDir(),
			Mode:       info.Mode(),
			Type:       tarEntryType(header),
			LinkTarget: header.Linkname,
		})
	}

	return entries, nil
}

// extractTarArchive extracts an entire tarball into destDir. Every write goes
// through an os.Root anchored at destDir, so the kernel refuses any path that
// escapes the destination (via ".." or a symlink component), race-free.
// Options overwrite/skipExisting/stripComponents/pattern/preservePermissions/
// maxBytes are honored; symlinks are recreated with an escape guard.
func extractTarArchive(tarPath, destDir string, options zipExtractOptions) error {
	reader, closer, err := openTarReader(tarPath)
	if err != nil {
		return err
	}
	defer closer.Close()

	absDest, err := filepath.Abs(destDir)
	if err != nil {
		return fmt.Errorf("Error resolving destination %s: %w", destDir, err)
	}
	if err := os.MkdirAll(absDest, 0755); err != nil {
		return fmt.Errorf("Error creating destination %s: %w", absDest, err)
	}
	root, err := os.OpenRoot(absDest)
	if err != nil {
		return fmt.Errorf("Error opening destination %s: %w", absDest, err)
	}
	defer root.Close()

	baseWithSep := ensureTrailingSeparator(absDest)
	budget := newByteBudget(options.maxBytes)

	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("Error reading %s: %w", tarPath, err)
		}

		entryName := normalizeZipEntryName(header.Name)
		if entryName == "" {
			continue
		}

		if options.pattern != "" {
			match, err := path.Match(options.pattern, entryName)
			if err != nil {
				return fmt.Errorf("Invalid tarExtract pattern '%s': %w", options.pattern, err)
			}
			if !match {
				continue
			}
		}

		stripped, err := stripZipComponents(entryName, options.stripComponents, header.FileInfo().IsDir())
		if err != nil {
			return err
		}
		if stripped == "" {
			continue
		}

		// Lexical containment as a cheap first layer; os.Root is the
		// authoritative, kernel-enforced check performed by the write below.
		target := filepath.Clean(filepath.Join(absDest, filepath.FromSlash(stripped)))
		if err := ensureWithinBase(target, absDest, baseWithSep); err != nil {
			return err
		}

		if err := writeTarEntryToRoot(root, absDest, reader, header, stripped, options.zipWriteOptions, budget); err != nil {
			return err
		}
	}

	return nil
}

// extractTarEntry extracts a single named entry (a file or a directory subtree)
// mirroring extractZipEntry. Because tar is a stream format it makes a single
// pass, collecting the file entry or the subtree as it goes. Writes are
// performed through an os.Root anchored at the destination.
func extractTarEntry(tarPath, entryPath, destPath string, options zipExtractEntryOptions) error {
	targetName := normalizeZipEntryName(entryPath)
	if targetName == "" {
		return fmt.Errorf("Entry path %s resolves to an empty name", entryPath)
	}

	reader, closer, err := openTarReader(tarPath)
	if err != nil {
		return err
	}
	defer closer.Close()

	prefix := targetName + "/"

	absDest, err := filepath.Abs(destPath)
	if err != nil {
		return fmt.Errorf("Error resolving destination %s: %w", destPath, err)
	}

	fileFound := false
	var root *os.Root
	defer func() {
		if root != nil {
			root.Close()
		}
	}()
	baseWithSep := ""
	budget := newByteBudget(options.maxBytes)

	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("Error reading %s: %w", tarPath, err)
		}

		name := normalizeZipEntryName(header.Name)

		if name == targetName && !header.FileInfo().IsDir() {
			// Single file entry: dest is the file path.
			if err := extractTarSingleFile(reader, header, absDest, options, budget); err != nil {
				return err
			}
			fileFound = true
			break
		}

		isSelfDir := name == targetName && header.FileInfo().IsDir()
		isChild := strings.HasPrefix(name, prefix)
		if !isSelfDir && !isChild {
			continue
		}

		// Directory subtree: dest is a directory that receives the subtree.
		if root == nil {
			if options.mkdirs {
				if err := os.MkdirAll(absDest, 0755); err != nil {
					return fmt.Errorf("Error creating destination %s: %w", absDest, err)
				}
			} else {
				info, err := os.Stat(absDest)
				if err != nil {
					return fmt.Errorf("Destination %s does not exist", absDest)
				}
				if !info.IsDir() {
					return fmt.Errorf("Destination %s is not a directory", absDest)
				}
			}
			r, err := os.OpenRoot(absDest)
			if err != nil {
				return fmt.Errorf("Error opening destination %s: %w", absDest, err)
			}
			root = r
			baseWithSep = ensureTrailingSeparator(absDest)
		}

		if isSelfDir {
			continue
		}

		relative := strings.TrimPrefix(name, prefix)
		if relative == "" {
			continue
		}

		target := filepath.Clean(filepath.Join(absDest, filepath.FromSlash(relative)))
		if err := ensureWithinBase(target, absDest, baseWithSep); err != nil {
			return err
		}

		if err := writeTarEntryToRoot(root, absDest, reader, header, relative, options.zipWriteOptions, budget); err != nil {
			return err
		}
	}

	if !fileFound && root == nil {
		return fmt.Errorf("Entry '%s' not found in %s", entryPath, tarPath)
	}
	return nil
}

func extractTarSingleFile(reader *tar.Reader, header *tar.Header, absDest string, options zipExtractEntryOptions, budget *byteBudget) error {
	parent := filepath.Dir(absDest)
	if options.mkdirs {
		if err := os.MkdirAll(parent, 0755); err != nil {
			return fmt.Errorf("Error creating parent directory %s: %w", parent, err)
		}
	} else {
		if _, err := os.Stat(parent); err != nil {
			return fmt.Errorf("Parent directory %s does not exist", parent)
		}
	}

	root, err := os.OpenRoot(parent)
	if err != nil {
		return fmt.Errorf("Error opening destination %s: %w", parent, err)
	}
	defer root.Close()

	return writeTarEntryToRoot(root, parent, reader, header, filepath.Base(absDest), options.zipWriteOptions, budget)
}

// writeTarEntryToRoot writes one tar entry (directory, file, or symlink) at the
// root-relative path relSlash (forward-slash form). All filesystem operations
// go through root, so the kernel guarantees the write cannot escape the
// destination via ".." or a symlink component. base is used only to build
// human-readable paths for error messages.
func writeTarEntryToRoot(root *os.Root, base string, reader *tar.Reader, header *tar.Header, relSlash string, options zipWriteOptions, budget *byteBudget) error {
	if err := rejectNULInPath(header.Name, header.Linkname); err != nil {
		return err
	}
	rel := filepath.FromSlash(relSlash)
	display := filepath.Join(base, rel)

	switch header.Typeflag {
	case tar.TypeDir:
		if err := root.MkdirAll(rel, 0755); err != nil {
			return fmt.Errorf("Error creating directory %s: %w", display, err)
		}
		if options.preservePermissions {
			if err := root.Chmod(rel, header.FileInfo().Mode().Perm()); err != nil && !errors.Is(err, os.ErrPermission) {
				return fmt.Errorf("Error setting permissions on %s: %w", display, err)
			}
		}
		return nil
	case tar.TypeSymlink:
		return writeTarSymlinkToRoot(root, base, header, relSlash, options)
	case tar.TypeReg, '\x00': // '\x00' is the legacy TypeRegA (deprecated) regular-file flag
		return writeTarRegularFileToRoot(root, base, reader, header, relSlash, options, budget)
	default:
		return fmt.Errorf("tarExtract cannot handle entry %s (unsupported type %q)", header.Name, string(header.Typeflag))
	}
}

// mkdirAllParent creates the parent directories of a root-relative slash path.
func mkdirAllParent(root *os.Root, relSlash, display string) error {
	dir := path.Dir(relSlash)
	if dir == "." || dir == "/" {
		return nil
	}
	if err := root.MkdirAll(filepath.FromSlash(dir), 0755); err != nil {
		return fmt.Errorf("Error creating parent directory for %s: %w", display, err)
	}
	return nil
}

func writeTarSymlinkToRoot(root *os.Root, base string, header *tar.Header, relSlash string, options zipWriteOptions) error {
	rel := filepath.FromSlash(relSlash)
	display := filepath.Join(base, rel)

	// os.Root blocks *traversal* through an escaping symlink at use time but
	// does not validate a link target at creation, so refuse escaping targets
	// here to fail closed and match the zip behavior.
	if symlinkTargetEscapes(header.Linkname, relSlash) {
		return fmt.Errorf("Refusing to extract symlink %s pointing outside destination (%s)", header.Name, header.Linkname)
	}

	if err := mkdirAllParent(root, relSlash, display); err != nil {
		return err
	}

	if _, err := root.Lstat(rel); err == nil {
		if options.skipExisting {
			return nil
		}
		if !options.overwrite {
			return fmt.Errorf("Destination %s already exists", display)
		}
		if err := root.Remove(rel); err != nil {
			return fmt.Errorf("Error replacing %s: %w", display, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := root.Symlink(header.Linkname, rel); err != nil {
		return fmt.Errorf("Error creating symlink %s: %w", display, err)
	}
	return nil
}

func writeTarRegularFileToRoot(root *os.Root, base string, reader *tar.Reader, header *tar.Header, relSlash string, options zipWriteOptions, budget *byteBudget) error {
	rel := filepath.FromSlash(relSlash)
	display := filepath.Join(base, rel)

	if err := mkdirAllParent(root, relSlash, display); err != nil {
		return err
	}

	mode := header.FileInfo().Mode().Perm()
	outFile, err := createExtractedFileInRoot(root, rel, mode, options, display)
	if err != nil {
		return err
	}
	if outFile == nil {
		return nil // skipExisting: the destination already exists
	}
	defer outFile.Close()

	if err := budget.copy(outFile, reader, header.Name); err != nil {
		return err
	}

	if options.preservePermissions {
		// Chmod via the open descriptor, not the path.
		if err := outFile.Chmod(mode); err != nil && !errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("Error setting permissions on %s: %w", display, err)
		}
	}
	return nil
}

// symlinkTargetEscapes reports whether a symlink target would point outside the
// extraction root, given the link's own root-relative location (slash form).
// Absolute targets, and relative targets that resolve above the root, are
// refused. The target is not otherwise simplified — its meaning depends on
// where the link lives.
func symlinkTargetEscapes(linkname, relSlash string) bool {
	if strings.HasPrefix(filepath.ToSlash(linkname), "/") || filepath.IsAbs(linkname) {
		return true
	}
	joined := path.Join(path.Dir(relSlash), filepath.ToSlash(linkname))
	return joined == ".." || strings.HasPrefix(joined, "../")
}

// readTarEntry reads a single entry's bytes without writing to disk, mirroring
// readZipEntry. Returns found=false when the entry is absent.
func readTarEntry(tarPath, entryPath string) ([]byte, bool, error) {
	target := normalizeZipEntryName(entryPath)
	if target == "" {
		return nil, false, fmt.Errorf("Entry path %s resolves to an empty name", entryPath)
	}

	reader, closer, err := openTarReader(tarPath)
	if err != nil {
		return nil, false, err
	}
	defer closer.Close()

	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, false, fmt.Errorf("Error reading %s: %w", tarPath, err)
		}

		name := normalizeZipEntryName(header.Name)
		if name != target {
			continue
		}

		if header.FileInfo().IsDir() {
			return nil, false, fmt.Errorf("tarRead cannot read directory entries (%s)", entryPath)
		}

		data, err := io.ReadAll(reader)
		if err != nil {
			return nil, false, err
		}
		return data, true, nil
	}

	return nil, false, nil
}
