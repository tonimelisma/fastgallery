package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsVideoFile(t *testing.T) {
	assert.Equal(t, isVideoFile("test.txt"), false, "false positive")
	assert.Equal(t, isVideoFile(""), false, "false positive")
	assert.Equal(t, isVideoFile("test.mp4"), true, "false negative")
	assert.Equal(t, isVideoFile("test.mov"), true, "false negative")
	assert.Equal(t, isVideoFile("test.3gp"), true, "false negative")
	assert.Equal(t, isVideoFile("test.avi"), true, "false negative")
	assert.Equal(t, isVideoFile("test.mts"), true, "false negative")
	assert.Equal(t, isVideoFile("test.m4v"), true, "false negative")
	assert.Equal(t, isVideoFile("test.mpg"), true, "false negative")
	assert.Equal(t, isVideoFile("test.MP4"), true, "false negative")
}

func TestIsImageFile(t *testing.T) {
	assert.Equal(t, isImageFile("test.txt"), false, "false positive")
	assert.Equal(t, isImageFile(""), false, "false positive")
	assert.Equal(t, isImageFile("test.jpg"), true, "false negative")
	assert.Equal(t, isImageFile("test.JPG"), true, "false negative")
	assert.Equal(t, isImageFile("test.jpeg"), true, "false negative")
	assert.Equal(t, isImageFile("test.heic"), true, "false negative")
	assert.Equal(t, isImageFile("test.png"), true, "false negative")
	assert.Equal(t, isImageFile("test.tif"), true, "false negative")
	assert.Equal(t, isImageFile("test.tiff"), true, "false negative")
}

func TestIsMediaFile(t *testing.T) {
	assert.Equal(t, isMediaFile("test.txt"), false, "false positive")
	assert.Equal(t, isMediaFile("test.jpg"), true, "false negative")
	assert.Equal(t, isMediaFile("test.mp4"), true, "false negative")
}

func TestStripExtension(t *testing.T) {
	assert.Equal(t, stripExtension("../tmp/filename.txt"), "../tmp/filename", "mismatch")
	assert.Equal(t, stripExtension("/tmp/filename.txt"), "/tmp/filename", "mismatch")
	assert.Equal(t, stripExtension("filename.txt"), "filename", "mismatch")
}
