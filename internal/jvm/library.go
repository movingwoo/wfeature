package jvm

import _ "embed"

const (
	// ObjectClass is the root, and the one class this VM answers without a
	// class file behind it.
	ObjectClass                = "java/lang/Object"
	ClassClass                 = "java/lang/Class"
	RunnableClass              = "java/lang/Runnable"
	StringClass                = "java/lang/String"
	StringBufferClass          = "java/lang/StringBuffer"
	StringBuilderClass         = "java/lang/StringBuilder"
	ThreadClass                = "java/lang/Thread"
	SystemClass                = "java/lang/System"
	IOExceptionClass           = "java/io/IOException"
	InputStreamClass           = "java/io/InputStream"
	ByteArrayInputStreamClass  = "java/io/ByteArrayInputStream"
	DataInputStreamClass       = "java/io/DataInputStream"
	OutputStreamClass          = "java/io/OutputStream"
	PrintStreamClass           = "java/io/PrintStream"
	ByteArrayOutputStreamClass = "java/io/ByteArrayOutputStream"
	DataOutputStreamClass      = "java/io/DataOutputStream"
	HashtableClass             = "java/util/Hashtable"
	VectorClass                = "java/util/Vector"
	RandomClass                = "java/util/Random"
)

//go:embed classdata/java/lang/Class.class
var classClass []byte

//go:embed classdata/java/lang/Runnable.class
var runnableClass []byte

//go:embed classdata/java/lang/String.class
var stringClass []byte

//go:embed classdata/java/lang/StringBuffer.class
var stringBufferClass []byte

//go:embed classdata/java/lang/StringBuilder.class
var stringBuilderClass []byte

//go:embed classdata/java/lang/Thread.class
var threadClass []byte

//go:embed classdata/java/lang/System.class
var systemClass []byte

//go:embed classdata/java/io/IOException.class
var ioExceptionClass []byte

//go:embed classdata/java/io/InputStream.class
var inputStreamClass []byte

//go:embed classdata/java/io/ByteArrayInputStream.class
var byteArrayInputStreamClass []byte

//go:embed classdata/java/io/DataInputStream.class
var dataInputStreamClass []byte

//go:embed classdata/java/io/OutputStream.class
var outputStreamClass []byte

//go:embed classdata/java/io/PrintStream.class
var printStreamClass []byte

//go:embed classdata/java/io/ByteArrayOutputStream.class
var byteArrayOutputStreamClass []byte

//go:embed classdata/java/io/DataOutputStream.class
var dataOutputStreamClass []byte

//go:embed classdata/java/util/Hashtable.class
var hashtableClass []byte

//go:embed classdata/java/util/Vector.class
var vectorClass []byte

//go:embed classdata/java/util/Random.class
var randomClass []byte

// CoreLibrary contains runtime-owned CLDC classes that require class metadata
// in addition to Go native methods. It always precedes application sources.
type CoreLibrary struct{}

func (CoreLibrary) ClassBytes(name string) ([]byte, bool) {
	switch name {
	case ClassClass:
		return classClass, true
	case RunnableClass:
		return runnableClass, true
	case StringClass:
		return stringClass, true
	case StringBufferClass:
		return stringBufferClass, true
	case StringBuilderClass:
		return stringBuilderClass, true
	case ThreadClass:
		return threadClass, true
	case SystemClass:
		return systemClass, true
	case IOExceptionClass:
		return ioExceptionClass, true
	case InputStreamClass:
		return inputStreamClass, true
	case ByteArrayInputStreamClass:
		return byteArrayInputStreamClass, true
	case DataInputStreamClass:
		return dataInputStreamClass, true
	case OutputStreamClass:
		return outputStreamClass, true
	case PrintStreamClass:
		return printStreamClass, true
	case ByteArrayOutputStreamClass:
		return byteArrayOutputStreamClass, true
	case DataOutputStreamClass:
		return dataOutputStreamClass, true
	case HashtableClass:
		return hashtableClass, true
	case VectorClass:
		return vectorClass, true
	case RandomClass:
		return randomClass, true
	default:
		return nil, false
	}
}
