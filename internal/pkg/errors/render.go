package errors

var ()

// RenderContextPrefix* are the prefixes commonly used to create a string used in
// UI errors outputs. If a prefix is used more than once, it should have a
// const created.
const (
	RenderContextDestDir  = "Destination Dir: "
	RenderContextDestFile = "Destination File: "
)
