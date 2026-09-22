package webassets

import "embed"

// Files contains all browser assets and server-rendered templates.
//
//go:embed all:admin all:covers all:public all:templates
var Files embed.FS
