package main

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"flag"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Nr90/imgsim"
	"github.com/pkg/errors"
	"github.com/rif/imgdup2go/hasher"
	"github.com/rivo/duplo"
	"github.com/vbauerster/mpb/v5"
	"github.com/vbauerster/mpb/v5/decor"
)

var (
	extensions  = map[string]func(io.Reader) (image.Image, error){"jpg": jpeg.Decode, "jpeg": jpeg.Decode, "png": png.Decode, "gif": gif.Decode}
	algo        = flag.String("algo", "avg", "algorithm for image hashing fmiq|avg|diff")
	sensitivity = flag.Int("sensitivity", 0, "the sensitivity treshold (the lower, the better the match (can be negative)) - fmiq algorithm only")
	path        = flag.String("path", ".", "the path to search the images")
	dryRun      = flag.Bool("dryrun", false, "only print found matches")
	undo        = flag.Bool("undo", false, "restore removed duplicates")
	recurse     = flag.Bool("r", false, "go through subdirectories as well")
)

const (
	DUPLICATES   = "duplicates"
	keepPrefix   = "_KEPT_"
	deletePrefix = "_GONE_"
)

type imgInfo struct {
	fileInfo string
	res      int
}

// CopyFile copies a file from src to dst. If src and dst files exist, and are
// the same, then return success. Otherise, attempt to create a hard link
// between the two files. If that fail, copy the file contents from src to dst.
func CopyFile(src, dst string) (err error) {
	sfi, err := os.Stat(src)
	if err != nil {
		return
	}
	if !sfi.Mode().IsRegular() {
		// cannot copy non-regular files (e.g., directories,
		// symlinks, devices, etc.)
		return fmt.Errorf("CopyFile: non-regular source file %s (%q)", sfi.Name(), sfi.Mode().String())
	}
	dfi, err := os.Stat(dst)
	if err != nil {
		if !os.IsNotExist(err) {
			return
		}
	} else {
		if !(dfi.Mode().IsRegular()) {
			return fmt.Errorf("CopyFile: non-regular destination file %s (%q)", dfi.Name(), dfi.Mode().String())
		}
		if os.SameFile(sfi, dfi) {
			return
		}
	}
	if err = os.Link(src, dst); err == nil {
		return
	}
	err = copyFileContents(src, dst)
	return
}

// copyFileContents copies the contents of the file named src to the file named
// by dst. The file will be created if it does not already exist. If the
// destination file exists, all it's contents will be replaced by the contents
// of the source file.
func copyFileContents(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return
	}
	defer checkClose(in, &err)
	out, err := os.Create(dst)
	if err != nil {
		return
	}
	defer checkClose(out, &err)
	if _, err = io.Copy(out, in); err != nil {
		return
	}
	err = out.Sync()
	return
}

func main() {
	flag.Parse()

	var buf bytes.Buffer
	logger := log.New(&buf, "logger: ", log.Lshortfile)
	dst := filepath.Join(*path, DUPLICATES)
	*sensitivity -= 100
	if *undo {
		handleUndo(logger, dst)
		return
	}
	p, bar := createProgressBar()
	files := findFilesToCompare(logger, bar)

	store := createEmptyStore()

	if !*dryRun {
		_, err := os.Stat(dst)
		if err != nil && os.IsNotExist(err) {
			if err := os.Mkdir(dst, os.ModePerm); err != nil {
				logger.Println("Could not create destination directory: ", err)
				os.Exit(1)
			}
		}
	}

	for filename := range files {
		err := handleFile(store, filename, logger, dst)
		if err != nil {
			logger.Print(err)
		}
		bar.Increment()
	}
	p.Wait()
	fmt.Print("Report:\n", &buf)
}

func findFilesToCompare(logger *log.Logger, bar *mpb.Bar) <-chan string {
	files := make(chan string, 1000)
	go func() {
		total := int64(0)
		defer close(files)
		err := filepath.Walk(*path, func(currentPath string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			// logger.Printf("file %s", currentPath)
			if !info.IsDir() {
				total++
				bar.SetTotal(total, false)
				logger.Printf("Found file: %v", info.Name())
				files <- currentPath
			}
			if info.IsDir() && !*recurse && currentPath != *path {
				return filepath.SkipDir
			}
			return nil
		})
		if err != nil {
			logger.Printf("Failed to read files: %v", err)
		}
		bar.SetTotal(total, true)
		logger.Printf("Found %d files\n", total)
	}()

	return files
}

func createEmptyStore() hasher.Store {
	// Create an empty store.
	var store hasher.Store
	switch *algo {
	case "fmiq":
		store = hasher.NewDuploStore(*sensitivity)
	default:
		store = hasher.NewImgsimStore()
	}
	return store
}

func handleFile(store hasher.Store, currentFile string, logger *log.Logger, dst string) (err error) {
	ext := filepath.Ext(currentFile)
	if len(ext) > 1 {
		ext = ext[1:]
	}
	if _, ok := extensions[ext]; !ok {
		return nil
	}
	format, err := getImageFormat(currentFile)
	if err != nil {
		return err
	}

	if decodeFunc, ok := extensions[format]; ok {
		logger.Printf("decoding %s", currentFile)
		file, err := os.Open(currentFile)
		if err != nil {
			return errors.WithMessagef(err, "%s", currentFile)
		}
		defer checkClose(file, &err)

		img, err := decodeFunc(file)
		if err != nil {
			return errors.WithMessagef(err, "ignoring %s", currentFile)
		}
		b := img.Bounds()
		res := b.Dx() * b.Dy()

		hash := createImageHash(img)
		match := store.Query(hash)
		if match != nil {
			matchedImgInfo := match.(*imgInfo)
			matchedFile := matchedImgInfo.fileInfo
			logger.Printf("%s matches: %s\n", currentFile, matchedFile)

			if !*dryRun {
				sum, err := createFileHash(currentFile, matchedFile)
				if err != nil {
					return err
				}
				if res > matchedImgInfo.res {
					store.Add(&imgInfo{fileInfo: currentFile, res: res}, hash)
					store.Delete(matchedFile, hash)
					if err := os.Rename(matchedFile, filepath.Join(dst, fmt.Sprintf("%s_%s_%s", sum, deletePrefix, matchedFile))); err != nil {
						return errors.WithMessagef(err, "error moving file: %s_%s_%s", sum, deletePrefix, matchedFile)
					}
					if err := CopyFile(currentFile, filepath.Join(dst, fmt.Sprintf("%s_%s_%s", sum, keepPrefix, currentFile))); err != nil {
						return errors.WithMessagef(err, "error copying file: %s_%s_%s", sum, keepPrefix, matchedFile)
					}
				} else {
					if err := CopyFile(matchedFile, filepath.Join(dst, fmt.Sprintf("%s_%s_%s", sum, keepPrefix, matchedFile))); err != nil {
						logger.Println("error copying file: " + fmt.Sprintf("%s_%s_%s", sum, keepPrefix, matchedFile))
						return errors.WithMessagef(err, "error copying file: %s_%s_%s", sum, keepPrefix, matchedFile)
					}
					if err := os.Rename(currentFile, filepath.Join(dst, fmt.Sprintf("%s_%s_%s", sum, deletePrefix, currentFile))); err != nil {
						return errors.WithMessagef(err, "error moving file: %s_%s_%s", sum, deletePrefix, matchedFile)
					}
				}
			} else {
				store.Add(&imgInfo{fileInfo: currentFile, res: res}, hash)
			}

		} else {
			store.Add(&imgInfo{fileInfo: currentFile, res: res}, hash)
		}
	}
	return err
}

func createFileHash(filename string, fi string) (string, error) {
	md5Hasher := md5.New()
	_, err := md5Hasher.Write([]byte(filename + fi))
	if err != nil {
		return "", errors.WithMessagef(err, "Failed to write to %q", filename+fi)
	}
	sum := hex.EncodeToString(md5Hasher.Sum(nil))[:5]
	return sum, nil
}

func checkClose(c io.Closer, err *error) {
	cErr := c.Close()
	if *err == nil {
		*err = cErr
	}
}

func createImageHash(img image.Image) interface{} {
	var hash interface{}
	switch *algo {
	case "fmiq":
		hash, _ = duplo.CreateHash(img)
	case "avg":
		hash = imgsim.AverageHash(img)
	case "diff":
		hash = imgsim.DifferenceHash(img)
	default:
		hash = imgsim.AverageHash(img)
	}
	return hash
}

func getImageFormat(fn string) (format string, err error) {
	file, err := os.Open(fn)
	if err != nil {
		return "", errors.WithMessagef(err, "%s", fn)
	}
	defer checkClose(file, &err)
	_, format, err = image.DecodeConfig(file)
	if err != nil {
		return "", errors.WithMessagef(err, "%s", fn)
	}
	return format, nil
}

func createProgressBar() (*mpb.Progress, *mpb.Bar) {
	p := mpb.New(
		// override default (80) width
		mpb.WithWidth(64),
		// override default 120ms refresh rate
		mpb.WithRefreshRate(180*time.Millisecond),
	)

	name := "Processed Images:"
	// Add a bar
	// You're not limited to just a single bar, add as many as you need
	bar := p.AddBar(int64(1),
		// override default "[=>-]" format
		mpb.BarStyle("╢▌▌░╟"),
		// Prepending decorators
		mpb.PrependDecorators(
			// display our name with one space on the right
			decor.Name(name, decor.WC{W: len(name) + 1, C: decor.DidentRight}),
			decor.CountersNoUnit("%d/%d"),
			decor.OnComplete(
				// ETA decorator with ewma age of 60, and width reservation of 4
				decor.EwmaETA(decor.ET_STYLE_GO, 60, decor.WC{W: 4}), " done",
			),
		),
		// Appending decorators
		mpb.AppendDecorators(
			// Percentage decorator with minWidth and no extra config
			decor.Percentage(),
		),
	)
	return p, bar
}

func handleUndo(logger *log.Logger, dst string) {
	files, err := ioutil.ReadDir(dst)
	if err != nil {
		log.Fatal(err)
	}
	for _, f := range files {
		if strings.Contains(f.Name(), keepPrefix) {
			if *dryRun {
				logger.Println("removing ", f.Name())
			} else {
				err := os.Remove(filepath.Join(dst, f.Name()))
				if err != nil {
					logger.Fatalf("Failed to remove file %q: %v", f.Name(), err)
				}
			}
		}
		if strings.Contains(f.Name(), deletePrefix) {
			if *dryRun {
				logger.Printf("moving %s to %s\n ", filepath.Join(dst, f.Name()), filepath.Join(*path, f.Name()[13:]))
			} else {
				err := os.Rename(filepath.Join(dst, f.Name()), filepath.Join(*path, f.Name()[13:]))
				if err != nil {
					logger.Fatalf("Failed to rename file %q: %v", f.Name(), err)
				}
			}
		}
	}
	if *dryRun {
		logger.Print("removing directory: ", dst)
	} else if err := os.Remove(dst); err != nil {
		logger.Print("could not remove duplicates folder: ", err)
	}
	os.Exit(0)
}
