package main

import (
	"embed"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cheggaaa/pb/v3"
	"github.com/davidbyttow/govips/v2/vips"
	"github.com/kr/pretty"

	"github.com/alexflint/go-arg"
)

// Embed all static assets
//go:embed assets
var assets embed.FS

// Define global exit function, so unit tests can override this
var exit = os.Exit

// configuration state is stored in this struct
type configuration struct {
	files struct {
		originalDir    string
		fullsizeDir    string
		thumbnailDir   string
		directoryMode  os.FileMode
		fileMode       os.FileMode
		imageExtension string
		videoExtension string
	}
	media struct {
		thumbnailWidth    int
		thumbnailHeight   int
		fullsizeMaxWidth  int
		fullsizeMaxHeight int
		videoMaxSize      int
	}
	concurrency int
}

// initialize the configuration with hardcoded defaults
func initializeConfig() (config configuration) {
	config.files.originalDir = "_original"
	config.files.fullsizeDir = "_fullsize"
	config.files.thumbnailDir = "_thumbnail"
	config.files.directoryMode = 0755
	config.files.fileMode = 0644
	config.files.imageExtension = ".jpg"
	config.files.videoExtension = ".mp4"

	config.media.thumbnailWidth = 280
	config.media.thumbnailHeight = 210
	config.media.fullsizeMaxWidth = 1920
	config.media.fullsizeMaxHeight = 1080
	config.media.videoMaxSize = 640

	config.concurrency = 8

	return config
}

// file struct represents an individual media file
// relPath is the relative path to from source/gallery root directory.
// For source files, exists marks whether it exists in the gallery and doesn't need to be copied.
// In this case, gallery has all three transformed files (original, full-size and thumbnail) and
// the thumbnail's modification date isn't before the original source file's.
// For gallery files, exists marks whether all three gallery files are in place (original, full-size
// and thumbnail) and there's a corresponding source file.
type file struct {
	name    string
	relPath string
	absPath string
	modTime time.Time
	exists  bool
}

// directory struct is one directory, which contains files and subdirectories
// relPath is the relative path from source/gallery root directory
type directory struct {
	name           string
	relPath        string
	absPath        string
	modTime        time.Time
	files          []file
	subdirectories []directory
}

// exists checks whether given file, directory or symlink exists
func exists(filepath string) bool {
	if _, err := os.Stat(filepath); os.IsNotExist(err) {
		return false
	}
	return true
}

// isDirectory checks whether provided path is a directory or symlink to one
// resolves symlinks only one level deep
func isDirectory(directory string) bool {
	filestat, err := os.Stat(directory)
	if os.IsNotExist(err) {
		return false
	}

	if filestat.IsDir() {
		return true
	}

	if filestat.Mode()&os.ModeSymlink != 0 {
		realDirectory, err := filepath.EvalSymlinks(directory)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
			return false
		}

		realFilestat, err := os.Stat(realDirectory)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
			return false
		}

		if realFilestat.IsDir() {
			return true
		}
	}

	return false
}

// Validate that source and gallery directories given as parameters
// are valid directories. Return absolue path of source and gallery
func validateSourceAndGallery(source string, gallery string) (string, string) {
	var err error

	source, err = filepath.Abs(source)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
		exit(1)
	}

	if !isDirectory(source) {
		fmt.Fprintf(os.Stderr, "Source directory doesn't exist: %s\n", source)
		exit(1)
	}

	gallery, err = filepath.Abs(gallery)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
		exit(1)
	}

	if !isDirectory(gallery) {
		// Ok, gallery isn't a directory but check whether the parent directory is
		// and we're supposed to create gallery there during runtime
		galleryParent, err := filepath.Abs(gallery + "/../")
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
			exit(1)
		}

		if !isDirectory(galleryParent) {
			fmt.Fprintf(os.Stderr, "Neither gallery directory or it's parent directory exist: %s\n", gallery)
			exit(1)
		}
	}

	return source, gallery
}

// Checks whether directory has media files, or subdirectories with media files.
// If there's a subdirectory that's empty or that has directories or files which
// aren't media files, we leave that out of the directory tree.
func dirHasMediafiles(directory string) (isEmpty bool) {
	list, err := os.ReadDir(directory)
	if err != nil {
		// If we can't read the directory contents, it doesn't have media files in it
		return false
	}

	if len(list) == 0 {
		// If it's empty, it doesn't have media files
		return false
	}

	for _, entry := range list {
		entryAbsPath := filepath.Join(directory, entry.Name())
		if entry.IsDir() {
			// Recursion to subdirectories
			if dirHasMediafiles(entryAbsPath) {
				return true
			}
		} else if isMediaFile(entryAbsPath) {
			// We found at least one media file, return true
			return true
		}
	}

	// Didn't find at least one media file
	return false
}

// Check whether given path is a video file
func isVideoFile(filename string) bool {
	switch filepath.Ext(strings.ToLower(filename)) {
	case ".mp4", ".mov", ".3gp", ".avi", ".mts", ".m4v", ".mpg":
		return true
	default:
		return false
	}
}

// Check whether given path is an image file
func isImageFile(filename string) bool {
	switch filepath.Ext(strings.ToLower(filename)) {
	case ".jpg", ".jpeg", ".heic", ".png", ".gif", ".tif", ".tiff":
		return true
	case ".cr2", ".raw", ".arw":
		return true
	default:
		return false
	}
}

// Check whether given absolute path is a media file
func isMediaFile(filename string) bool {
	if isImageFile(filename) {
		return true
	}

	// TODO optIgnoreVideos
	if isVideoFile(filename) {
		return true
	}

	return false
}

// Create a recursive directory struct by traversing the directory absoluteDirectory.
// The function calls itself recursively, carrying state in the relativeDirectory parameter.
func createDirectoryTree(absoluteDirectory string, parentDirectory string) (tree directory) {
	// In case the target directory doesn't exist, it's the gallery directory
	// which hasn't been created yet. We'll just create a dummy tree and return it.
	if !exists(absoluteDirectory) && parentDirectory == "" {
		tree.name = filepath.Base(absoluteDirectory)
		tree.relPath = parentDirectory
		tree.absPath, _ = filepath.Abs(absoluteDirectory)
		return
	}

	// Fill in the directory name and other basic info
	tree.name = filepath.Base(absoluteDirectory)
	tree.absPath, _ = filepath.Abs(absoluteDirectory)
	tree.relPath = parentDirectory
	absoluteDirectoryStat, _ := os.Stat(absoluteDirectory)
	tree.modTime = absoluteDirectoryStat.ModTime()

	// List directory contents
	list, err := os.ReadDir(absoluteDirectory)
	if err != nil {
		log.Fatal("Couldn't list directory contents:", absoluteDirectory)
	}

	// If it's a directory and it has media files somewhere, add it to directories
	// If it's a media file, add it to the files
	for _, entry := range list {
		entryAbsPath := filepath.Join(absoluteDirectory, entry.Name())
		entryRelPath := filepath.Join(parentDirectory, entry.Name())
		if entry.IsDir() {
			if dirHasMediafiles(entryAbsPath) {
				entrySubTree := createDirectoryTree(entryAbsPath, entryRelPath)
				tree.subdirectories = append(tree.subdirectories, entrySubTree)
			}
		} else if isMediaFile(entryAbsPath) {
			entryFileInfo, err := entry.Info()
			if err != nil {
				log.Fatal("Couldn't stat file information for media file:", entry.Name())
			}
			entryFile := file{
				name:    entry.Name(),
				relPath: entryRelPath,
				absPath: entryAbsPath,
				modTime: entryFileInfo.ModTime(),
				exists:  false,
			}
			tree.files = append(tree.files, entryFile)
		}
	}
	return
}

// stripExtension strips the filename extension and returns the basename
func stripExtension(filename string) string {
	extension := filepath.Ext(filename)
	return filename[0 : len(filename)-len(extension)]
}

func reservedDirectory(path string, config configuration) bool {
	if path == config.files.thumbnailDir {
		return true
	}

	if path == config.files.fullsizeDir {
		return true
	}

	if path == config.files.originalDir {
		return true
	}

	return false
}

// compareDirectoryTrees compares two directory trees (source and gallery) and marks
// each file that exists in both
func compareDirectoryTrees(source *directory, gallery *directory, config configuration) {
	for i, sourceFile := range source.files {
		sourceFileBasename := stripExtension(sourceFile.name)

		var thumbnailFile, fullsizeFile, originalFile *file

		// Go through all subdirectories, and check the ones that match
		// the thumbnail, full-size or original subdirectories
		for _, subDir := range gallery.subdirectories {
			if subDir.name == config.files.thumbnailDir {
				for _, outputFile := range subDir.files {
					outputFileBasename := stripExtension(outputFile.name)
					if sourceFileBasename == outputFileBasename {
						thumbnailFile = &outputFile
					}
				}
			}

			if subDir.name == config.files.fullsizeDir {
				for _, outputFile := range subDir.files {
					outputFileBasename := stripExtension(outputFile.name)
					if sourceFileBasename == outputFileBasename {
						fullsizeFile = &outputFile
					}
				}
			}

			if subDir.name == config.files.originalDir {
				for _, outputFile := range subDir.files {
					outputFileBasename := stripExtension(outputFile.name)
					if sourceFileBasename == outputFileBasename {
						originalFile = &outputFile
					}
				}
			}
		}

		// If all of thumbnail, full-size and original files exist in gallery, mark them
		// as existing so they aren't deleted in a clean-up. If only 1 or 2 of 3 exist,
		// they might be remnants of an aborted previous run. Clean-up will take care of them.
		if thumbnailFile != nil && fullsizeFile != nil && originalFile != nil {
			thumbnailFile.exists = true
			fullsizeFile.exists = true
			originalFile.exists = true
			if !thumbnailFile.modTime.Before(sourceFile.modTime) {
				// Overwrite gallery files in case source file's been updated since the thumbnail
				// was created.
				source.files[i].exists = true
			}
		}
	}

	// After checking all the files in this directory, recurse into each subdirectory and do the same
	for k, inputDir := range source.subdirectories {
		if !reservedDirectory(inputDir.name, config) {
			for l, outputDir := range gallery.subdirectories {
				if inputDir.name == outputDir.name {
					compareDirectoryTrees(&(source.subdirectories[k]), &(gallery.subdirectories[l]), config)
				}
			}
		}
	}
}

func countChanges(source directory) (outputChanges int) {
	outputChanges = 0
	for _, file := range source.files {
		if !file.exists {
			outputChanges++
		}
	}

	for _, dir := range source.subdirectories {
		outputChanges = outputChanges + countChanges(dir)
	}

	return outputChanges
}

func createDirectory(destination string, dryRun bool, dirMode os.FileMode) {
	if _, err := os.Stat(destination); os.IsNotExist(err) {
		if dryRun {
			fmt.Println("Would create directory:", destination)
		} else {
			err := os.Mkdir(destination, dirMode)
			if err != nil {
				fmt.Fprintf(os.Stderr, "couldn't create directory %s: %s\n", destination, err.Error())
				exit(1)
			}
		}
	}
}

// TODO deprecate copy() function
func copy(sourceDir string, destDir string, filename string, dryRun bool) {
	sourceFilename := filepath.Join(sourceDir, filename)
	destFilename := filepath.Join(destDir, filename)

	if !dryRun {
		_, err := os.Stat(sourceFilename)
		if err != nil {
			fmt.Fprintf(os.Stderr, "couldn't copy source file %s: %s\n", sourceFilename, err.Error())
			exit(1)
		}

		sourceHandle, err := os.Open(sourceFilename)
		if err != nil {
			fmt.Fprintf(os.Stderr, "couldn't open source file for copy %s: %s\n", sourceFilename, err.Error())
			exit(1)
		}
		defer sourceHandle.Close()

		destHandle, err := os.Create(destFilename)
		if err != nil {
			fmt.Fprintf(os.Stderr, "couldn't create dest file %s: %s\n", destFilename, err.Error())
			exit(1)
		}
		defer destHandle.Close()

		_, err = io.Copy(destHandle, sourceHandle)
		if err != nil {
			fmt.Fprintf(os.Stderr, "couldn't copy file %s -> %s: %s\n", sourceFilename, destFilename, err.Error())
			exit(1)
		}
	} else {
		fmt.Println("would copy", sourceFilename, "to", destFilename)
	}
}

// copyRootAssets copies all the embedded assets to the root directory of the gallery
func copyRootAssets(gallery directory, dryRun bool, fileMode os.FileMode) {
	// TODO add dry-run
	assetDirectoryListing, err := assets.ReadDir("assets")
	if err != nil {
		log.Fatal("couldn't open embedded assets:", err.Error())
	}

	// Iterate through all the embedded assets
	for _, entry := range assetDirectoryListing {
		if !entry.IsDir() {
			switch filepath.Ext(strings.ToLower(entry.Name())) {
			// Copy all javascript and CSS files
			case ".js", ".css":
				if !dryRun {
					filebuffer, err := assets.ReadFile("assets/" + entry.Name())
					if err != nil {
						log.Fatal("couldn't open embedded asset:", entry.Name(), ":", err.Error())
					}
					err = os.WriteFile(gallery.absPath+"/"+entry.Name(), filebuffer, fileMode)
					if err != nil {
						log.Fatal("couldn't write embedded asset:", gallery.absPath+"/"+entry.Name(), ":", err.Error())
					}
				} else {
					fmt.Println("Would copy JS/CSS file", entry.Name(), "to", gallery.absPath)
				}
			}

			switch entry.Name() {
			// Copy back.png and folder.png
			case "back.png", "folder.png":
				if !dryRun {
					filebuffer, err := assets.ReadFile("assets/" + entry.Name())
					if err != nil {
						log.Fatal("couldn't open embedded asset:", entry.Name(), ":", err.Error())
					}
					err = os.WriteFile(gallery.absPath+"/"+entry.Name(), filebuffer, fileMode)
					if err != nil {
						log.Fatal("couldn't write embedded asset:", gallery.absPath+"/"+entry.Name(), ":", err.Error())
					}
				} else {
					fmt.Println("Would copy icon", entry.Name(), "to", gallery.absPath)
				}
			}
		}
	}
}

func createGallery(depth int, source directory, gallery directory, dryRun bool, config configuration) {
	// TODO

	// createHTML(depth, source.subdirectories, source.files)
}

func main() {
	// Define command-line arguments
	var args struct {
		Source  string `arg:"positional,required" help:"Source directory for images/videos"`
		Gallery string `arg:"positional,required" help:"Destination directory to create gallery in"`
		Verbose bool   `arg:"-v,--verbose" help:"verbosity level"`
		DryRun  bool   `arg:"--dry-run" help:"dry run; don't change anything, just print what would be done"`
	}

	// Parse command-line arguments
	arg.MustParse(&args)

	// Validate source and gallery arguments, make paths absolute
	args.Source, args.Gallery = validateSourceAndGallery(args.Source, args.Gallery)

	// Initialize configuration (assets, directories, file types)
	config := initializeConfig()

	fmt.Println("Creating gallery...")
	fmt.Println("Source:", args.Source)
	fmt.Println("Gallery:", args.Gallery)
	fmt.Println()
	fmt.Println("Finding all media files...")

	// Creating a directory struct of both source as well as gallery directories
	source := createDirectoryTree(args.Source, "")
	gallery := createDirectoryTree(args.Gallery, "")

	// Check which source media exists in gallery
	compareDirectoryTrees(&source, &gallery, config)

	// Count number of source files which don't exist in gallery
	changes := countChanges(source)
	if changes > 0 {
		fmt.Println(changes, "files to update")
		if !exists(gallery.absPath) {
			createDirectory(gallery.absPath, args.DryRun, config.files.directoryMode)
		}

		var progressBar *pb.ProgressBar
		if !args.DryRun {
			progressBar = pb.StartNew(changes)
			if args.Verbose {
				vips.LoggingSettings(nil, vips.LogLevelDebug)
				vips.Startup(&vips.Config{
					CacheTrace:   false,
					CollectStats: false,
					ReportLeaks:  true})
			} else {
				vips.LoggingSettings(nil, vips.LogLevelError)
				vips.Startup(nil)
			}
			defer vips.Shutdown()
		}

		createGallery(0, source, gallery, args.DryRun, config)
		copyRootAssets(gallery, args.DryRun, config.files.fileMode)

		if !args.DryRun {
			progressBar.Finish()
		}

		fmt.Println("Gallery updated!")
	} else {
		fmt.Println("Gallery already up to date!")
	}

	fmt.Println("source:")
	pretty.Print(source)

	fmt.Println("gallery:")
	pretty.Print(gallery)
}
