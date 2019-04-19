# imgdup2go
Another command line visual (perceptual) image duplicate finder

```
Usage of ./imgdup2go:
  -algo string
    	algorithm for image hashing fmiq|avg|diff (default "avg")
  -dryrun
    	only print found matches
  -path string
    	the path to search the images (default ".")
  -sensitivity int
    	the sensitivity treshold (the lower, the better the match (can be negative)) - fmiq algorithm only
  -undo
    	restore removed duplicates
```

Upon running imgdup2go will create a ``duplicates`` directory where it will put the found duplicate files with an informative prefix, eg:

```
7563e__GONE__image1.jpg
7563e__KEPT__image92.jpg
f469b__GONE__image16.jpg
f469b__KEPT__image19.jpg
```

The initial hash pairs the files together, the KEPT files were copied from the initial directory while the GONE files were moved.

After inspecting the pairs, if you agree with what the tool found as duplicates you can just remove the duplicates folder; otherwise, move the specific GONE files back into the original directory, removing the prefix.
To undo everything you can use the -undo flag.

The algorithms can be switched using -algo flag, in my test I found [avg](http://www.hackerfactor.com/blog/?/archives/432-Looks-Like-It.html) (default) to provide the best results.

For [fmqi](http://grail.cs.washington.edu/projects/query/mrquery.pdf) (Fast Multiresolution Image Querying) you can detect more loosely similar images by increasing the sensitivity, to make it even stricter you can go negative. Enjoy!

## Install

You can find binaries [here](https://github.com/rif/imgdup2go/releases).

From source:
```
go get -u -v github.com/rif/imgdup2go
```
## Credits
The heavy lifting is done by [imgsim](https://github.com/Nr90/imgsim) and [duplo](https://github.com/rivo/duplo) libraries. You can find more info on the hashing algorithms at the libraries home page.

A python version can be found [here](https://github.com/rif/imgdup)

## WARNING
This tool moves and deletes files. Please make a backup of your image collection before using it!
