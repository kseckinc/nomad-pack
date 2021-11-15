package filesystem

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/hashicorp/nomad-pack/internal/pkg/errors"
	"github.com/hashicorp/nomad-pack/internal/pkg/logging"
)

// CopyFile copies a file from one path to another
func CopyFile(sourcePath, destinationPath string, logger logging.Logger) (err error) {
	// Open the source file
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		logger.Debug(fmt.Sprintf(errors.ErrOpeningSourceFile.Error()+": %s", err))
		return
	}

	// Set up a deferred close handler
	defer func() {
		if err = sourceFile.Close(); err != nil {
			logger.Debug(fmt.Sprintf(errors.ErrClosingSourceFile.Error()+": %s", err))
		}
	}()

	// Open the destination file
	destinationFile, err := os.Create(destinationPath)
	if err != nil {
		logger.Debug(fmt.Sprintf(errors.ErrOpeningSourceFile.Error()+": %s", err))
		return
	}
	// Set up a deferred close handler
	defer func() {
		if err = destinationFile.Close(); err != nil {
			logger.Debug(fmt.Sprintf(errors.ErrClosingSourceFile.Error()+": %s", err))
		}
	}()

	// Copy the file
	_, err = io.Copy(destinationFile, sourceFile)
	if err != nil {
		logger.Debug(fmt.Sprintf("error copying file: %s", err))
		return
	}

	// Sync the file contents
	err = destinationFile.Sync()
	if err != nil {
		logger.Debug(fmt.Sprintf("error syncing destination file: %s", err))
		return
	}

	// Get the source file info so we can copy the permissions
	sourceFileInfo, err := os.Stat(sourcePath)
	if err != nil {
		logger.Debug(fmt.Sprintf("error getting source file info: %s", err))
		return
	}

	// Set the destination file permissions from the source file mode
	err = os.Chmod(destinationPath, sourceFileInfo.Mode())
	if err != nil {
		logger.Debug(fmt.Sprintf("error getting setting destination file permissions: %s", err))
		return
	}

	// Give the defer functions a chance to set this variable
	return
}

// CopyDir recursively copies a directory.
func CopyDir(sourceDir string, destinationDir string, logger logging.Logger) (err error) {
	// Clean the directory paths
	sourceDir = filepath.Clean(sourceDir)
	destinationDir = filepath.Clean(destinationDir)

	// Get the source directory info to validate that it is a directory
	sourceDirInfo, err := os.Stat(sourceDir)
	if err != nil {
		logger.Debug(fmt.Sprintf("error getting source directory info: %s", err))
		return
	}

	// Throw error if not a directory
	// TODO: Might need to handle symlinks.
	if !sourceDirInfo.IsDir() {
		err = fmt.Errorf("source is not a directory")
		logger.Debug(err.Error())
		return
	}

	// Make sure the destination directory doesn't already exist
	_, err = os.Stat(destinationDir)
	if err != nil && !os.IsNotExist(err) {
		logger.Debug(fmt.Sprintf("error getting destination file info: %s", err))
		return
	}
	// throw error if it does exist
	if err == nil {
		err = fmt.Errorf("destination already exists")
		logger.Debug(err.Error())
		return
	}

	// Make the destination direction and copy the file permissions
	err = os.MkdirAll(destinationDir, sourceDirInfo.Mode())
	if err != nil {
		logger.Debug(fmt.Sprintf("error creating destination directory: %s", err))
		return
	}

	// Read the contents of the source directory
	sourceEntries, err := os.ReadDir(sourceDir)
	if err != nil {
		logger.Debug(fmt.Sprintf("error reading source directory entries: %s", err))
		return
	}

	// Iterate over all the directory entries and copy them
	for _, sourceEntry := range sourceEntries {
		// Build the source and destination paths
		sourcePath := filepath.Join(sourceDir, sourceEntry.Name())
		destinationPath := filepath.Join(destinationDir, sourceEntry.Name())

		// If a directory, then recurse, else copy all files
		if sourceEntry.IsDir() {
			err = CopyDir(sourcePath, destinationPath, logger)
			if err != nil {
				return
			}
		} else {
			// Skip symlinks.
			if sourceEntry.Type()&os.ModeSymlink != 0 {
				continue
			}

			// Copy file from source directory to destination directory
			err = CopyFile(sourcePath, destinationPath, logger)
			if err != nil {
				return
			}
		}
	}

	return nil
}

// IsDir returns true if the given path is an existing directory.
func IsDir(path string, emptyPathIsValid bool) bool {
	if path == "" {
		return emptyPathIsValid
	}

	if pathAbs, err := filepath.Abs(path); err == nil {
		if fileInfo, err := os.Stat(pathAbs); !errors.Is(err, os.ErrNotExist) && fileInfo.IsDir() {
			return true
		}
	}

	return false
}

// Exists returns true if the given path is has a `os.Stat`-able object.
func Exists(path string, emptyPathIsValid bool) bool {
	if path == "" {
		return emptyPathIsValid
	}

	if pathAbs, err := filepath.Abs(path); err == nil {
		if _, err := os.Stat(pathAbs); errors.Is(err, os.ErrNotExist) {
			return false
		}
	}

	return true
}

// This WriteFile implementation will check to see if the file exists before
// trying to overwrite it.
func WriteFile(destination string, content string, overwrite bool) error {
	// Check to see if the file already exists and validate against the value
	// of overwrite.

	info, err := os.Stat(destination)
	pathErr := os.PathError{
		Op:   "writefile",
		Path: destination,
	}

	if err == nil && !overwrite {
		pathErr.Err = fs.ErrExist
		return &pathErr
	}
	if info != nil && info.IsDir() {
		pathErr.Err = fmt.Errorf("destination is a directory")
		return &pathErr
	}

	err = os.WriteFile(destination, []byte(content), 0644)

	if err != nil {
		return err
	}

	return nil
}

// CreatePath creates a nested directory if it does not exist. The behavior
// can be toggled to emit an error when the directory already exists.
func CreatePath(path string, errIfExists bool) error {
	// Check to see if the file already exists and handle errIfExists.
	info, err := os.Stat(path)
	if err == nil && info.IsDir() && errIfExists {
		return &ErrDirExists{
			Op:   "mkdir",
			Path: path,
			Err:  fmt.Errorf("directory already exists"),
		}
	}

	return os.MkdirAll(path, 0755)
}

type ErrDirExists struct {
	Op   string
	Path string
	Err  error
}

func (ede ErrDirExists) Is(target error) bool {
	return target == fs.ErrExist
}

func (ede ErrDirExists) Error() string {
	return fmt.Sprintf("%s %s: %w", ede.Op, ede.Path, ede.Err)
}
