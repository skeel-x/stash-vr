package static

import "embed"

//go:embed *.gohtml *.html *.png *.css *.js
var Fs embed.FS
