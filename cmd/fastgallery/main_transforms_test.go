package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestTransform(t *testing.T) {
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
	if err != nil {
		t.Error("cp error", err.Error())
	}

}
