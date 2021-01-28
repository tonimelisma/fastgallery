package main

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

var exitCount = 0

func testExit(ret int) {
	exitCount = exitCount + 1
	return
}
func TestValidateSourceAndGallery(t *testing.T) {
	originalExit := exit
	defer func() { exit = originalExit }()
	exit = testExit

	tempDir, err := ioutil.TempDir("", "fastgallery-test-")
	if err != nil {
		t.Error("couldn't create temporary directory")
	}
	defer os.RemoveAll(tempDir)

	exitCountBefore := exitCount
	_, _ = validateSourceAndGallery(tempDir+"/nonexistent", tempDir+"/gallery")
	assert.EqualValues(t, exitCountBefore+1, exitCount, "validateArgs did not exit")

	exitCountBefore = exitCount
	_, _ = validateSourceAndGallery(tempDir, tempDir+"/gallery/nonexistent")
	assert.EqualValues(t, exitCountBefore+1, exitCount, "validateArgs did not exit")

	exitCountBefore = exitCount
	_, _ = validateSourceAndGallery(tempDir, tempDir+"/gallery")
	assert.EqualValues(t, exitCountBefore, exitCount, "validateArgs did not exit")
}

func TestIsDirectory(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "fastgallery-test-")
	if err != nil {
		t.Error("couldn't create temporary directory")
	}
	defer os.RemoveAll(tempDir)

	err = os.Mkdir(tempDir+"/subdir", 0755)
	if err != nil {
		t.Error("couldn't create subdirectory")
	}
	defer os.RemoveAll(tempDir + "/subdir")
	assert.True(t, isDirectory(tempDir+"/subdir"))

	err = os.Symlink(tempDir+"/subdir", tempDir+"/symlink")
	if err != nil {
		t.Error("couldn't create symlink")
	}
	defer os.RemoveAll(tempDir + "/symlink")
	assert.True(t, isDirectory(tempDir+"/symlink"))

	emptyFile, err := os.Create(tempDir + "/file")
	if err != nil {
		t.Error("couldn't create symlink")
	}
	defer emptyFile.Close()
	defer os.RemoveAll(tempDir + "/file")
	assert.False(t, isDirectory(tempDir+"/file"))

	assert.False(t, isDirectory(tempDir+"/nonexistent"))
}

func TestExists(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "fastgallery-test-")
	if err != nil {
		t.Error("couldn't create temporary directory")
	}
	defer os.RemoveAll(tempDir)

	emptyFile, err := os.Create(tempDir + "/file")
	if err != nil {
		t.Error("couldn't create symlink")
	}
	defer emptyFile.Close()
	defer os.RemoveAll(tempDir + "/file")
	assert.True(t, exists(tempDir+"/file"))

	assert.False(t, exists(tempDir+"/nonexistent"))
}

func TestDirHasMediaFiles(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "fastgallery-test-")
	if err != nil {
		t.Error("couldn't create temporary directory")
	}
	defer os.RemoveAll(tempDir)

	emptyFile, err := os.Create(tempDir + "/file.jpg")
	if err != nil {
		t.Error("couldn't create symlink")
	}
	defer emptyFile.Close()
	defer os.RemoveAll(tempDir + "/file.jpg")

	assert.True(t, dirHasMediafiles(tempDir))
}

func TestDirHasMediaFilesFailing(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "fastgallery-test-")
	if err != nil {
		t.Error("couldn't create temporary directory")
	}
	defer os.RemoveAll(tempDir)

	emptyFile, err := os.Create(tempDir + "/file.txt")
	if err != nil {
		t.Error("couldn't create symlink")
	}
	defer emptyFile.Close()
	defer os.RemoveAll(tempDir + "/file.txt")

	assert.False(t, dirHasMediafiles(tempDir))
}

func TestDirHasMediaFilesRecurse(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "fastgallery-test-")
	if err != nil {
		t.Error("couldn't create temporary directory")
	}
	defer os.RemoveAll(tempDir)

	err = os.Mkdir(tempDir+"/subdir", 0755)
	if err != nil {
		t.Error("couldn't create subdirectory")
	}
	defer os.RemoveAll(tempDir + "/subdir")

	emptyFile, err := os.Create(tempDir + "/subdir/file.jpg")
	if err != nil {
		t.Error("couldn't create symlink")
	}
	defer emptyFile.Close()
	defer os.RemoveAll(tempDir + "/subdir/file.jpg")

	assert.True(t, dirHasMediafiles(tempDir))
}

func TestDirHasMediaFilesRecurseFailing(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "fastgallery-test-")
	if err != nil {
		t.Error("couldn't create temporary directory")
	}
	defer os.RemoveAll(tempDir)

	err = os.Mkdir(tempDir+"/subdir", 0755)
	if err != nil {
		t.Error("couldn't create subdirectory")
	}
	defer os.RemoveAll(tempDir + "/subdir")

	emptyFile, err := os.Create(tempDir + "/subdir/file.txt")
	if err != nil {
		t.Error("couldn't create symlink")
	}
	defer emptyFile.Close()
	defer os.RemoveAll(tempDir + "/subdir/file.txt")

	assert.False(t, dirHasMediafiles(tempDir))
}

func TestIsXxxFile(t *testing.T) {
	assert.True(t, isVideoFile("test.mp4"))
	assert.False(t, isVideoFile("test.jpg"))
	assert.False(t, isVideoFile("test.txt"))
	assert.True(t, isImageFile("test.jpg"))
	assert.False(t, isImageFile("test.mp4"))
	assert.False(t, isImageFile("test.txt"))
	assert.True(t, isMediaFile("test.mp4"))
	assert.True(t, isMediaFile("test.jpg"))
	assert.False(t, isMediaFile("test.txt"))
}

// TODO tests for
// createDirectoryTree
// stripExtension
// compareDirectoryTrees
//   - exists, doesn't exist, some gallery files exist / some don't
//   - thumbnail modified earlier than original or vice versa
