package main

import (
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
	"github.com/rif/imgdup2go/hasher"
	"github.com/rivo/duplo"
	"github.com/vbauerster/mpb"
	"github.com/vbauerster/mpb/decor"
)

var (
	extensions   = map[string]func(io.Reader) (image.Image, error){"jpg": jpeg.Decode, "jpeg": jpeg.Decode, "png": png.Decode, "gif": gif.Decode}
	dst          = "duplicates"
	keepPrefix   = "_KEPT_"
	deletePrefix = "_GONE_"
	algo         = flag.String("algo", "fmiq", "algorithm for image hashing fmiq|avf|diff")
	sensitivity  = flag.Int("sensitivity", 0, "the sensitivity treshold (the lower, the better the match (can be negative))")
	path         = flag.String("path", ".", "the path to search the images")
	dryRun       = flag.Bool("dryrun", false, "only print found matches")
	undo         = flag.Bool("undo", false, "restore removed duplicates")
)

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
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return
	}
	defer func() {
		cerr := out.Close()
		if err == nil {
			err = cerr
		}
	}()
	if _, err = io.Copy(out, in); err != nil {
		return
	}
	err = out.Sync()
	return
}

func main() {
	flag.Parse()
	dst = filepath.Join(*path, dst)
	*sensitivity -= 100
	if *undo {
		files, err := ioutil.ReadDir(dst)
		if err != nil {
			log.Fatal(err)
		}
		for _, f := range files {
			if strings.Contains(f.Name(), keepPrefix) {
				if *dryRun {
					fmt.Println("removing ", f.Name())
				} else {
					os.Remove(filepath.Join(dst, f.Name()))
				}
			}
			if strings.Contains(f.Name(), deletePrefix) {
				if *dryRun {
					fmt.Printf("moving %s to %s\n ", filepath.Join(dst, f.Name()), filepath.Join(*path, f.Name()[13:]))
				} else {
					os.Rename(filepath.Join(dst, f.Name()), filepath.Join(*path, f.Name()[13:]))
				}
			}
		}
		if *dryRun {
			fmt.Print("removing directory: ", dst)
		} else {
			if err := os.Remove(dst); err != nil {
				fmt.Print("could not remove duplicates folder: ", err)
			}
		}
		os.Exit(0)
	}

	files, err := ioutil.ReadDir(*path)
	if err != nil {
		log.Fatal(err)
	}

	// Create an empty store.
	var store hasher.Store
	switch *algo {
	case "fmiq":
		store = hasher.NewDuploStore(*sensitivity)
	default:
		store = hasher.NewImgsimStore()
	}

	fmt.Printf("Found %d files\n", len(files))

	p := mpb.New(
		// override default (80) width
		mpb.WithWidth(100),
		// override default "[=>-]" format
		mpb.WithFormat("╢▌▌░╟"),
		// override default 120ms refresh rate
		mpb.WithRefreshRate(100*time.Millisecond),
	)

	name := "Processed Images:"
	// Add a bar
	// You're not limited to just a single bar, add as many as you need
	bar := p.AddBar(int64(len(files)),
		// Prepending decorators
		mpb.PrependDecorators(
			// display our name with one space on the right
			decor.Name(name, decor.WC{W: len(name) + 1, C: decor.DidentRight}),
			// replace ETA decorator with "done" message, OnComplete event
			decor.OnComplete(
				// ETA decorator with ewma age of 60, and width reservation of 4
				decor.EwmaETA(decor.ET_STYLE_GO, 60, decor.WC{W: 4}), "done",
			),
		),
		// Appending decorators
		mpb.AppendDecorators(decor.Percentage()),
	)

	for _, f := range files {
		ext := filepath.Ext(f.Name())
		if len(ext) > 1 {
			ext = ext[1:]
		}
		if _, ok := extensions[ext]; !ok {
			bar.Increment()
			continue
		}
		fn := filepath.Join(*path, f.Name())
		file, err := os.Open(fn)
		if err != nil {
			fmt.Printf("%s: %v\n", fn, err)
			bar.Increment()
			continue
		}
		_, format, err := image.DecodeConfig(file)
		if err != nil {
			fmt.Printf("%s: %v\n", fn, err)
			file.Close()
			bar.Increment()
			continue
		}
		file.Close()

		if decodeFunc, ok := extensions[format]; ok {
			file, err := os.Open(fn)
			if err != nil {
				fmt.Printf("%s: %v\n", fn, err)
				bar.Increment()
				continue
			}

			img, err := decodeFunc(file)
			if err != nil {
				fmt.Printf("ignoring %s: %v\n", fn, err)
				bar.Increment()
				continue
			}
			// Add image "img" to the store.
			var hash interface{}

			switch *algo {
			case "fmiq":
				hash, _ = duplo.CreateHash(img)
			case "avg":
				hash = imgsim.AverageHash(img)
			case "diff":
				hash = imgsim.DifferenceHash(img)
			default:
				hash = imgsim.DifferenceHash(img)
			}
			match := store.Query(hash)
			if match != nil {
				fi := match.(os.FileInfo)
				fmt.Printf("%s matches: %s\n", fn, fi.Name())

				if !*dryRun {
					_, err := os.Stat(dst)
					if err != nil && os.IsNotExist(err) {
						if err := os.Mkdir(dst, os.ModePerm); err != nil {
							fmt.Println("Could not create destination directory: ", err)
							os.Exit(1)
						}
					}

					hasher := md5.New()
					hasher.Write([]byte(f.Name() + fi.Name()))
					sum := hex.EncodeToString(hasher.Sum(nil))[:5]
					if f.Size() >= fi.Size() {
						store.Add(f, hash)
						store.Delete(fi, hash)
						if err := os.Rename(filepath.Join(*path, fi.Name()), filepath.Join(dst, fmt.Sprintf("%s_%s_%s", sum, deletePrefix, fi.Name()))); err != nil {
							fmt.Println("error moving file: " + fmt.Sprintf("%s_%s_%s", sum, deletePrefix, fi.Name()))
						}
						if err := CopyFile(filepath.Join(*path, f.Name()), filepath.Join(dst, fmt.Sprintf("%s_%s_%s", sum, keepPrefix, f.Name()))); err != nil {
							fmt.Println("error copying file: " + fmt.Sprintf("%s_%s_%s", sum, keepPrefix, f.Name()))
						}
					} else {
						if err := CopyFile(filepath.Join(*path, fi.Name()), filepath.Join(dst, fmt.Sprintf("%s_%s_%s", sum, keepPrefix, fi.Name()))); err != nil {
							fmt.Println("error copying file: " + fmt.Sprintf("%s_%s_%s", sum, keepPrefix, fi.Name()))
						}
						if err := os.Rename(filepath.Join(*path, f.Name()), filepath.Join(dst, fmt.Sprintf("%s_%s_%s", sum, deletePrefix, f.Name()))); err != nil {
							fmt.Println("error moving file: " + fmt.Sprintf("%s_%s_%s", sum, deletePrefix, f.Name()))
						}
					}
				} else {
					store.Add(f, hash)
				}
			} else {
				store.Add(f, hash)
			}
			if err := file.Close(); err != nil {
				fmt.Println("could not close file: ", fn)
			}
			bar.Increment()
		}
	}
	p.Wait()
}
