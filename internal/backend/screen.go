package backend

// MinScreenSide and MaxScreenSide bound a handset screen anything may name:
// a page's start, the CLI's -screen, and the declaration a KTF descriptor
// carries. The floor is smaller than any handset these games shipped on and
// the ceiling is larger, because what this bounds is an allocation from a
// message or an archive rather than a taste in phones. They are kept here,
// below every Host and every platform, so that a request and a declaration
// are read by one range rather than by copies that can drift apart.
const (
	MinScreenSide = 32
	MaxScreenSide = 1024
)

// ValidScreen reports whether both sides are inside that range.
func ValidScreen(width, height int) bool {
	return ValidScreenSide(width) && ValidScreenSide(height)
}

// ValidScreenSide reports whether one side is inside that range.
func ValidScreenSide(side int) bool {
	return side >= MinScreenSide && side <= MaxScreenSide
}
