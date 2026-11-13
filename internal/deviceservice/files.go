package deviceservice

import "os"

func isMissing(e error) bool { return os.IsNotExist(e) }
