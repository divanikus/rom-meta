package emumovies

// missingPlatformDirectoryError means a successful parent listing established
// that no matching platform directory exists. Failed listings are ordinary errors.
type missingPlatformDirectoryError string

func (err missingPlatformDirectoryError) Error() string { return string(err) }
