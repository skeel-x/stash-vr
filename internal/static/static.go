package static

import "embed"

//go:embed *.gohtml *.png *.css *.js
var Fs embed.FS
