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

// createTarWriter creates a tar archive for writing, gzip-compressing when the
// destination extension calls for it. The returned finish function must be
// called (not deferred with a discarded error) to flush and close every layer.
func createTarWriter(tarPath string) (*tar.Writer, func() error, error) {
	if err := os.MkdirAll(filepath.Dir(tarPath), 0755); err != nil {
		return nil, nil, fmt.Errorf("Error creating parent directory for %s: %w", tarPath, err)
	}

	output, err := os.Create(tarPath)
	if err != nil {
		return nil, nil, fmt.Errorf("Error creating %s: %w", tarPath, err)
	}

	if isGzipTarget(tarPath) {
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
func tarDirectory(sourceDir, tarPath string, preserveRoot bool) error {
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
	return buildTarFromEntries([]zipPackItem{packItem}, tarPath)
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
func buildTarFromEntries(items []zipPackItem, tarPath string) error {
	if len(items) == 0 {
		return fmt.Errorf("tarPack requires at least one entry")
	}

	tarAbs, err := filepath.Abs(tarPath)
	if err != nil {
		return fmt.Errorf("Error resolving %s: %w", tarPath, err)
	}

	tw, finish, err := createTarWriter(tarPath)
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
	linkTarget := ""
	if info.Mode()&os.ModeSymlink != 0 {
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

	if info.Mode()&os.ModeSymlink != 0 || info.IsDir() {
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("tarPack cannot archive %s: unsupported file type", sourcePath)
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

// extractTarArchive extracts an entire tarball, honoring the same option set as
// extractZipArchive (overwrite/skipExisting/stripComponents/pattern/
// preservePermissions). Symlinks are recreated with an escape guard.
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
	baseWithSep := ensureTrailingSeparator(absDest)

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

		target := filepath.Join(absDest, filepath.FromSlash(stripped))
		target = filepath.Clean(target)
		if err := ensureWithinBase(target, absDest, baseWithSep); err != nil {
			return err
		}

		if err := writeTarEntryToDisk(reader, header, target, options.zipWriteOptions, true, absDest, baseWithSep); err != nil {
			return err
		}
	}

	return nil
}

// extractTarEntry extracts a single named entry (a file or a directory subtree)
// mirroring extractZipEntry. Because tar is a stream format it makes a single
// pass, collecting the file entry or the subtree as it goes.
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
	dirCreated := false
	baseWithSep := ""

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
			if err := extractTarSingleFile(reader, header, absDest, options); err != nil {
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
		if !dirCreated {
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
			baseWithSep = ensureTrailingSeparator(absDest)
			dirCreated = true
		}

		if isSelfDir {
			continue
		}

		relative := strings.TrimPrefix(name, prefix)
		if relative == "" {
			continue
		}

		target := filepath.Join(absDest, filepath.FromSlash(relative))
		target = filepath.Clean(target)
		if err := ensureWithinBase(target, absDest, baseWithSep); err != nil {
			return err
		}

		if err := writeTarEntryToDisk(reader, header, target, options.zipWriteOptions, true, absDest, baseWithSep); err != nil {
			return err
		}
	}

	if !fileFound && !dirCreated {
		return fmt.Errorf("Entry '%s' not found in %s", entryPath, tarPath)
	}
	return nil
}

func extractTarSingleFile(reader *tar.Reader, header *tar.Header, absDest string, options zipExtractEntryOptions) error {
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

	baseWithSep := ensureTrailingSeparator(parent)
	return writeTarEntryToDisk(reader, header, absDest, options.zipWriteOptions, false, parent, baseWithSep)
}

// writeTarEntryToDisk writes one tar entry (directory, file, or symlink) to
// destPath. For symlinks it validates that the link target cannot escape the
// destination root before creating it.
func writeTarEntryToDisk(reader *tar.Reader, header *tar.Header, destPath string, options zipWriteOptions, ensureParents bool, base, baseWithSep string) error {
	switch header.Typeflag {
	case tar.TypeDir:
		if ensureParents {
			if err := os.MkdirAll(destPath, 0755); err != nil {
				return fmt.Errorf("Error creating directory %s: %w", destPath, err)
			}
		} else if err := os.Mkdir(destPath, 0755); err != nil && !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("Error creating directory %s: %w", destPath, err)
		}
		if options.preservePermissions {
			if err := os.Chmod(destPath, header.FileInfo().Mode().Perm()); err != nil && !errors.Is(err, os.ErrPermission) {
				return fmt.Errorf("Error setting permissions on %s: %w", destPath, err)
			}
		}
		return nil
	case tar.TypeSymlink:
		return writeTarSymlink(header, destPath, options, ensureParents, base, baseWithSep)
	case tar.TypeReg, '\x00': // '\x00' is the legacy TypeRegA (deprecated) regular-file flag
		return writeTarRegularFile(reader, header, destPath, options, ensureParents)
	default:
		return fmt.Errorf("tarExtract cannot handle entry %s (unsupported type %q)", header.Name, string(header.Typeflag))
	}
}

func writeTarSymlink(header *tar.Header, destPath string, options zipWriteOptions, ensureParents bool, base, baseWithSep string) error {
	// Resolve the link target relative to the symlink's own directory and
	// ensure it stays within the destination root.
	linkDir := filepath.Dir(destPath)
	var resolved string
	if filepath.IsAbs(header.Linkname) {
		resolved = filepath.Clean(header.Linkname)
	} else {
		resolved = filepath.Clean(filepath.Join(linkDir, filepath.FromSlash(header.Linkname)))
	}
	if err := ensureWithinBase(resolved, base, baseWithSep); err != nil {
		return fmt.Errorf("Refusing to extract symlink %s pointing outside destination (%s)", header.Name, header.Linkname)
	}

	parentDir := filepath.Dir(destPath)
	if ensureParents {
		if err := os.MkdirAll(parentDir, 0755); err != nil {
			return fmt.Errorf("Error creating parent directory %s: %w", parentDir, err)
		}
	} else if _, err := os.Stat(parentDir); err != nil {
		return fmt.Errorf("Parent directory %s does not exist", parentDir)
	}

	if _, err := os.Lstat(destPath); err == nil {
		if options.skipExisting {
			return nil
		}
		if !options.overwrite {
			return fmt.Errorf("Destination %s already exists", destPath)
		}
		if err := os.Remove(destPath); err != nil {
			return fmt.Errorf("Error replacing %s: %w", destPath, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.Symlink(header.Linkname, destPath); err != nil {
		return fmt.Errorf("Error creating symlink %s: %w", destPath, err)
	}
	return nil
}

func writeTarRegularFile(reader *tar.Reader, header *tar.Header, destPath string, options zipWriteOptions, ensureParents bool) error {
	parentDir := filepath.Dir(destPath)
	if ensureParents {
		if err := os.MkdirAll(parentDir, 0755); err != nil {
			return fmt.Errorf("Error creating parent directory %s: %w", parentDir, err)
		}
	} else {
		if _, err := os.Stat(parentDir); err != nil {
			return fmt.Errorf("Parent directory %s does not exist", parentDir)
		}
	}

	if options.skipExisting {
		if _, err := os.Stat(destPath); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	} else {
		if _, err := os.Stat(destPath); err == nil && !options.overwrite {
			return fmt.Errorf("Destination %s already exists", destPath)
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	mode := header.FileInfo().Mode().Perm()
	outFile, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer outFile.Close()

	if _, err := io.Copy(outFile, reader); err != nil {
		return err
	}

	if options.preservePermissions {
		if err := os.Chmod(destPath, mode); err != nil && !errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("Error setting permissions on %s: %w", destPath, err)
		}
	}
	return nil
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
