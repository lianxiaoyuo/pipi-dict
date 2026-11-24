//go:build windows

package main

import _ "embed"

//go:embed assets/dictionary.db.gz
var embeddedDictionary []byte

//go:embed assets/ECDICT_LICENSE.txt
var dictionaryLicense string
