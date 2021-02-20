package main

import (
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

	tempDir, err := os.MkdirTemp("", "fastgallery-test-")
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
	tempDir, err := os.MkdirTemp("", "fastgallery-test-")
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
	tempDir, err := os.MkdirTemp("", "fastgallery-test-")
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
	tempDir, err := os.MkdirTemp("", "fastgallery-test-")
	if err != nil {
		t.Error("couldn't create temporary directory")
	}
	defer os.RemoveAll(tempDir)

	emptyFile, err := os.Create(tempDir + "/file.raw")
	if err != nil {
		t.Error("couldn't create symlink")
	}
	defer emptyFile.Close()
	defer os.RemoveAll(tempDir + "/file.raw")

	assert.True(t, dirHasMediafiles(tempDir))
}

func TestDirHasMediaFilesFailing(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "fastgallery-test-")
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
	tempDir, err := os.MkdirTemp("", "fastgallery-test-")
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
	tempDir, err := os.MkdirTemp("", "fastgallery-test-")
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

func TestCopyRootAssets(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "fastgallery-test-")
	if err != nil {
		t.Error("couldn't create temporary directory")
	}
	defer os.RemoveAll(tempDir)

	var tempGallery directory
	tempGallery.absPath = tempDir

	copyRootAssets(tempGallery, false, 0644)

	assert.FileExists(t, tempDir+"/back.png")
	assert.FileExists(t, tempDir+"/folder.png")
	assert.FileExists(t, tempDir+"/fastgallery.css")
	assert.FileExists(t, tempDir+"/fastgallery.js")
	assert.FileExists(t, tempDir+"/feather.min.js")
	assert.FileExists(t, tempDir+"/primer.css")
}

func TestStripExtension(t *testing.T) {
	assert.Equal(t, "file", stripExtension("file.jpg"))
	assert.NotEqual(t, "file", stripExtension("file/"))
}

func TestReservedDirectory(t *testing.T) {
	myConfig := initializeConfig()

	assert.True(t, reservedDirectory(myConfig.files.thumbnailDir, myConfig))
	assert.True(t, reservedDirectory(myConfig.files.fullsizeDir, myConfig))
	assert.True(t, reservedDirectory(myConfig.files.originalDir, myConfig))
	assert.False(t, reservedDirectory("diipadaapa", myConfig))
}

func TestCreateDirectory(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "fastgallery-test-")
	if err != nil {
		t.Error("couldn't create temporary directory")
	}
	defer os.RemoveAll(tempDir)

	myConfig := initializeConfig()

	createDirectory(tempDir+"/xyz", true, myConfig.files.directoryMode)
	assert.NoDirExists(t, tempDir+"/xyz")

	createDirectory(tempDir+"/xyz", false, myConfig.files.directoryMode)
	assert.DirExists(t, tempDir+"/xyz")
	os.RemoveAll(tempDir + "/xyz")
}

func TestCreateDirectoryTree(t *testing.T) {
	myConfig := initializeConfig()

	tempDir, err := os.MkdirTemp("", "fastgallery-test-")
	if err != nil {
		t.Error("couldn't create temporary directory")
	}
	defer os.RemoveAll(tempDir)

	// Create source directory with two files, a subdir with third file
	err = os.Mkdir(tempDir+"/source", 0755)
	if err != nil {
		t.Error("couldn't create source subdirectory")
	}
	defer os.RemoveAll(tempDir + "/source")

	emptyFile, err := os.Create(tempDir + "/source/file.jpg")
	if err != nil {
		t.Error("couldn't create file")
	}
	defer emptyFile.Close()
	defer os.RemoveAll(tempDir + "/source/file.jpg")

	emptyFile2, err := os.Create(tempDir + "/source/file2.jpg")
	if err != nil {
		t.Error("couldn't create file2")
	}
	defer emptyFile2.Close()
	defer os.RemoveAll(tempDir + "/source/file2.jpg")

	err = os.Mkdir(tempDir+"/source/subdir", 0755)
	if err != nil {
		t.Error("couldn't create source subdirectory's subdirectory")
	}
	defer os.RemoveAll(tempDir + "/source/subdir")

	emptyFile3, err := os.Create(tempDir + "/source/subdir/file.jpg")
	if err != nil {
		t.Error("couldn't create file in subdir")
	}
	defer emptyFile3.Close()
	defer os.RemoveAll(tempDir + "/source/subdir/file.jpg")

	// Create gallery subdirectory with one matching file
	err = os.Mkdir(tempDir+"/gallery", 0755)
	if err != nil {
		t.Error("couldn't create gallery subdirectory")
	}
	defer os.RemoveAll(tempDir + "/gallery")

	err = os.Mkdir(tempDir+"/gallery/"+myConfig.files.fullsizeDir, 0755)
	if err != nil {
		t.Error("couldn't create gallery subdirectory for fullsize")
	}
	defer os.RemoveAll(tempDir + "/gallery/" + myConfig.files.fullsizeDir)

	err = os.Mkdir(tempDir+"/gallery/"+myConfig.files.thumbnailDir, 0755)
	if err != nil {
		t.Error("couldn't create gallery subdirectory for thumbnail")
	}
	defer os.RemoveAll(tempDir + "/gallery/" + myConfig.files.thumbnailDir)

	err = os.Mkdir(tempDir+"/gallery/"+myConfig.files.originalDir, 0755)
	if err != nil {
		t.Error("couldn't create gallery subdirectory for original")
	}
	defer os.RemoveAll(tempDir + "/gallery/" + myConfig.files.originalDir)

	emptyFile4, err := os.Create(tempDir + "/gallery/" + myConfig.files.originalDir + "/file.jpg")
	if err != nil {
		t.Error("couldn't create original gallery file")
	}
	defer emptyFile4.Close()
	defer os.RemoveAll(tempDir + "/gallery/" + myConfig.files.originalDir + "/file.jpg")

	emptyFile5, err := os.Create(tempDir + "/gallery/" + myConfig.files.thumbnailDir + "/file.jpg")
	if err != nil {
		t.Error("couldn't create original gallery file")
	}
	defer emptyFile5.Close()
	defer os.RemoveAll(tempDir + "/gallery/" + myConfig.files.thumbnailDir + "/file.jpg")

	emptyFile6, err := os.Create(tempDir + "/gallery/" + myConfig.files.fullsizeDir + "/file.jpg")
	if err != nil {
		t.Error("couldn't create original gallery file")
	}
	defer emptyFile6.Close()
	defer os.RemoveAll(tempDir + "/gallery/" + myConfig.files.fullsizeDir + "/file.jpg")

	source := createDirectoryTree(tempDir+"/source", "")
	gallery := createDirectoryTree(tempDir+"/gallery", "")

	compareDirectoryTrees(&source, &gallery, myConfig)

	changes := countChanges(source)

	assert.EqualValues(t, 2, changes)
}

// TODO tests for
// createGallery
//   - exists, doesn't exist, some gallery files exist / some don't
//   - thumbnail modified earlier than original or vice versa
