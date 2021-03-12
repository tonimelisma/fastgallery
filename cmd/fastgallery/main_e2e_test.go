package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/davidbyttow/govips/v2/vips"
	"github.com/stretchr/testify/assert"
)

func TestE2E(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "fastgallery-test-")
	if err != nil {
		t.Error("couldn't create temporary directory")
	}
	defer os.RemoveAll(tempDir)

	cpCommand := exec.Command("cp", "-r", "../../testing/source", filepath.Join(tempDir, "source"))
	cpCommandOutput, err := cpCommand.CombinedOutput()
	if len(cpCommandOutput) > 0 {
		t.Error("cp produced output", string(cpCommandOutput))
	}
	assert.NoError(t, err)

	config := initializeConfig()

	source := createDirectoryTree(filepath.Join(tempDir, "source"), "", true)
	gallery := createDirectoryTree(filepath.Join(tempDir, "gallery"), "", true)
	compareDirectoryTrees(&source, &gallery, config)
	sourceChanges := countChanges(source, config)
	assert.EqualValues(t, 9, sourceChanges)
	galleryChanges := countChanges(gallery, config)
	assert.EqualValues(t, 0, galleryChanges)

	vips.LoggingSettings(nil, vips.LogLevelWarning)
	//log.SetOutput(io.Discard)
	vips.Startup(nil)

	createDirectory(gallery.absPath, false, config.files.directoryMode)
	updateMediaFiles(0, source, gallery, false, true, config, nil)

	// Gallery created, test that files are in order
	fullsizeFilename1 := filepath.Join(tempDir, "gallery", config.files.fullsizeDir, "panorama.heic")
	fullsizeFilename1 = stripExtension(fullsizeFilename1) + config.files.imageExtension
	assert.FileExists(t, fullsizeFilename1)

	fullsizeFilename2 := filepath.Join(tempDir, "gallery", config.files.fullsizeDir, "dog.heic")
	fullsizeFilename2 = stripExtension(fullsizeFilename2) + config.files.imageExtension
	assert.FileExists(t, fullsizeFilename2)

	thumbnailFilename1 := filepath.Join(tempDir, "gallery", "subdir", config.files.thumbnailDir, "gate.heic")
	thumbnailFilename1 = stripExtension(thumbnailFilename1) + config.files.imageExtension
	assert.FileExists(t, thumbnailFilename1)

	originalFilename1 := filepath.Join(tempDir, "gallery", "subdir", "subsubdir", config.files.originalDir, "recorder.heic")
	assert.FileExists(t, originalFilename1)

	missingHTMLFiles := findMissingHTMLFiles(gallery, config)
	assert.EqualValues(t, true, missingHTMLFiles)

	// create HTML
	updateHTMLFiles(0, source, gallery, false, true, config)

	missingHTMLFiles = findMissingHTMLFiles(gallery, config)
	assert.EqualValues(t, false, missingHTMLFiles)

	// Make changes and re-test
	sourceFilename1 := filepath.Join(tempDir, "source", "street.jpg")
	err = os.Chtimes(sourceFilename1, time.Now().Local(), time.Now().Local())
	assert.NoError(t, err)

	sourceFilename2 := filepath.Join(tempDir, "source", "cranes.jpg")
	err = os.Chtimes(sourceFilename2, time.Now().Local(), time.Now().Local())
	assert.NoError(t, err)

	sourceFilename3 := filepath.Join(tempDir, "source", "dog.heic")
	err = os.RemoveAll(sourceFilename3)
	assert.NoError(t, err)

	source = createDirectoryTree(filepath.Join(tempDir, "source"), "", true)
	gallery = createDirectoryTree(filepath.Join(tempDir, "gallery"), "", true)
	compareDirectoryTrees(&source, &gallery, config)
	sourceChanges = countChanges(source, config)
	assert.EqualValues(t, 2, sourceChanges)
	galleryChanges = countChanges(gallery, config)
	assert.EqualValues(t, 3, galleryChanges)



	// update without cleanup in gallery
	updateMediaFiles(0, source, gallery, false, true, config, nil)
	assert.FileExists(t, fullsizeFilename2)

	// cleanup gallery
	cleanUp(gallery, false, config)
	assert.NoFileExists(t, fullsizeFilename2)

	// update HTML
	updateHTMLFiles(0, source, gallery, false, true, config)

	missingHTMLFiles = findMissingHTMLFiles(gallery, config)
	assert.EqualValues(t, false, missingHTMLFiles)
}
