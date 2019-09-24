package common

import "regexp"

// hex encoded bytes32, with 0x prefix
var RootRegex, _ = regexp.Compile("^0x[0-9a-f]{64}$")

// make sure keys don't start with `__`, or hyphens/underscores at all
var KeyRegex, _ = regexp.Compile("^[0-9a-zA-Z][-_0-9a-zA-Z]{0,128}$")

// versions are not used as keys in firestore, and may contain dots.
var VersionRegex, _ = regexp.Compile("^[0-9a-zA-Z][-_.0-9a-zA-Z]{0,128}$")
