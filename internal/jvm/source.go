package jvm

// ClassSources resolves classes from the first source that contains them.
// Put platform libraries before application archives to preserve parent-first
// loading for classes owned by the runtime.
type ClassSources []ClassSource

func (sources ClassSources) ClassBytes(name string) ([]byte, bool) {
	for _, source := range sources {
		if source == nil {
			continue
		}
		if data, ok := source.ClassBytes(name); ok {
			return data, true
		}
	}
	return nil, false
}
