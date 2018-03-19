# imgdup2go
Another visual image duplicate finder

```
Usage of ./imgdup2go:
  -dryrun
    	only print found matches
  -path string
    	the path to search the images (default ".")
  -sensitivity int
    	the sensitivity treshold (the lower, the better the match (can be negative))
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

The initial hash pairs the files together, the KEPT files were copyed from the initial directory while the GONE files were moved.

After inspecting the pairs if you agree with what the tool found as duplicates you can just remove the duplicates folder, otherwhise move the specific GONE files back in the original directory removing the prefix.
To undo everything you can use the -undo flag.

To find more loosely similat images you can increase the sensitivity, to make it even stricter you can go negative. Enjoy!
