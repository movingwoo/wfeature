package clet

import "github.com/movingwoo/wfeature/internal/wipic"

// systemProperty answers a WIPI system property. The subscriber-number
// properties come from the handset number this session runs as; the rest from
// the shared property table.
func (client *Client) systemProperty(name string) (string, bool) {
	if name == "PHONENUMBER" || name == "MIN" {
		return client.subscriberNumber, true
	}
	value, ok := wipic.SystemProperties[name]
	return value, ok
}

// MappedSize is how much guest memory the image occupies: its code and the BSS
// that follows.
func (image ClientImage) MappedSize() uint64 {
	return uint64(len(image.Data)) + uint64(image.BSSSize)
}

// collectForAllocation is a no-op for a Clet: there is no Java heap to collect,
// so an allocation that the arena cannot satisfy simply fails.
func (runtime *initializationRuntime) collectForAllocation() bool { return false }

// collectHostResources is a no-op for a Clet: the Host-side objects the KTF
// collector frees — bound Java objects, image surfaces — a Clet never makes.
func (runtime *initializationRuntime) collectHostResources() {}
