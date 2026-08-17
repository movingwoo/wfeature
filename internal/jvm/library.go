package jvm

const (
	// ObjectClass is the root, and the one class this VM answers without any
	// declaration behind it.
	ObjectClass                = "java/lang/Object"
	ClassClass                 = "java/lang/Class"
	RunnableClass              = "java/lang/Runnable"
	StringClass                = "java/lang/String"
	StringBufferClass          = "java/lang/StringBuffer"
	StringBuilderClass         = "java/lang/StringBuilder"
	ThreadClass                = "java/lang/Thread"
	SystemClass                = "java/lang/System"
	ThrowableClass             = "java/lang/Throwable"
	IntegerClass               = "java/lang/Integer"
	LongClass                  = "java/lang/Long"
	ByteClass                  = "java/lang/Byte"
	MathClass                  = "java/lang/Math"
	RuntimeClass               = "java/lang/Runtime"
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
	CalendarClass              = "java/util/Calendar"
	DateClass                  = "java/util/Date"
	EnumerationClass           = "java/util/Enumeration"
	StackClass                 = "java/util/Stack"
	// ArrayEnumerationClass is the runtime's own Enumeration over a snapshot
	// of a collection, which is what Hashtable's two views answer with. It
	// lives outside java/util because no game names it: a game holds the
	// interface.
	ArrayEnumerationClass = "net/wfeature/ArrayEnumeration"
)
