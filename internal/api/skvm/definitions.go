package skvm

import "github.com/movingwoo/wfeature/internal/jvm"

// The SKVM class surface, declared rather than compiled. A method with no body
// here is a native the platform registers, which is where the Host — the save
// directory, the audio sink, the screen — actually is.

func definitions() []jvm.ClassDefinition {
	return append(micro3DDefinitions(), []jvm.ClassDefinition{
		{
			Name:      "com/skt/m/AudioClip",
			SuperName: "java/lang/Object",
			Access:    jvm.AccessPublic | jvm.AccessInterface | jvm.AccessAbstract,
			Methods: []jvm.MethodDefinition{
				{Name: "open", Descriptor: "([BII)V", Access: jvm.AccessPublic | jvm.AccessAbstract, Throws: []string{"com/skt/m/UnsupportedFormatException"}},
				{Name: "close", Descriptor: "()V", Access: jvm.AccessPublic | jvm.AccessAbstract},
				{Name: "play", Descriptor: "()V", Access: jvm.AccessPublic | jvm.AccessAbstract},
				{Name: "loop", Descriptor: "()V", Access: jvm.AccessPublic | jvm.AccessAbstract},
				{Name: "pause", Descriptor: "()V", Access: jvm.AccessPublic | jvm.AccessAbstract},
				{Name: "resume", Descriptor: "()V", Access: jvm.AccessPublic | jvm.AccessAbstract},
				{Name: "stop", Descriptor: "()V", Access: jvm.AccessPublic | jvm.AccessAbstract},
			},
		},
		{
			Name:      "com/skt/m/AudioSystem",
			SuperName: "java/lang/Object",
			Access:    jvm.AccessPublic,
			Methods: []jvm.MethodDefinition{
				{Name: "getAudioClip", Descriptor: "(Ljava/lang/String;)Lcom/skt/m/AudioClip;", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative, Throws: []string{"com/skt/m/UnsupportedFormatException"}},
				{Name: "getVolume", Descriptor: "()I", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "setVolume", Descriptor: "(I)V", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
			},
		},
		{
			Name:      "com/skt/m/BackLight",
			SuperName: "java/lang/Object",
			Access:    jvm.AccessPublic,
			Methods: []jvm.MethodDefinition{
				{Name: "on", Descriptor: "(I)V", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "off", Descriptor: "()V", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "getColor", Descriptor: "()I", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "setColor", Descriptor: "(I)V", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
			},
		},
		{
			Name:      "com/skt/m/Call",
			SuperName: "java/lang/Object",
			Access:    jvm.AccessPublic,
			Methods: []jvm.MethodDefinition{
				{Name: "call", Descriptor: "(Ljava/lang/String;)Z", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
			},
		},
		{
			Name:      "com/skt/m/Device",
			SuperName: "java/lang/Object",
			Access:    jvm.AccessPublic,
			Fields: []jvm.FieldDefinition{
				{Name: "NAI_MOBILE", Descriptor: "I", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessFinal, Constant: jvm.IntValue(0)},
			},
			Methods: []jvm.MethodDefinition{
				{Name: "beep", Descriptor: "(II)V", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "setNAI", Descriptor: "(I)V", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
			},
		},
		{
			Name:      "com/skt/m/Graphics2D",
			SuperName: "java/lang/Object",
			Access:    jvm.AccessPublic,
			Fields: []jvm.FieldDefinition{
				{Name: "SRC_COPY", Descriptor: "I", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessFinal, Constant: jvm.IntValue(0)},
				{Name: "SRC_AND", Descriptor: "I", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessFinal, Constant: jvm.IntValue(1)},
				{Name: "SRC_OR", Descriptor: "I", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessFinal, Constant: jvm.IntValue(2)},
				{Name: "SRC_XOR", Descriptor: "I", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessFinal, Constant: jvm.IntValue(3)},
			},
			Methods: []jvm.MethodDefinition{
				{Name: "<init>", Descriptor: "(Ljavax/microedition/lcdui/Graphics;)V", Access: jvm.AccessPublic, Body: forwardInit(Graphics2DClass, "init", "(Ljavax/microedition/lcdui/Graphics;)V")},
				{Name: "init", Descriptor: "(Ljavax/microedition/lcdui/Graphics;)V", Access: jvm.AccessPrivate | jvm.AccessNative},
				{Name: "getPixel", Descriptor: "(II)I", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "setPixel", Descriptor: "(III)V", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "getPixelMask", Descriptor: "(II)Z", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "setPixelMask", Descriptor: "(IIZ)V", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "invertRect", Descriptor: "(IIII)V", Access: jvm.AccessPublic | jvm.AccessNative},
				// captureLCD is static: it copies the screen, not the surface
				// a wrapper was built around, and three local titles call it
				// with invokestatic before they have made a wrapper at all.
				{Name: "captureLCD", Descriptor: "(IIII)Ljavax/microedition/lcdui/Image;", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "drawImage", Descriptor: "(IILjavax/microedition/lcdui/Image;IIIII)V", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "getGraphics2D", Descriptor: "(Ljavax/microedition/lcdui/Graphics;)Lcom/skt/m/Graphics2D;", Access: jvm.AccessPublic | jvm.AccessStatic, Body: graphics2DFor},
				{Name: "createMaskableImage", Descriptor: "(II)Ljavax/microedition/lcdui/Image;", Access: jvm.AccessPublic | jvm.AccessStatic, Body: createMaskableImage},
			},
		},
		{
			Name:      "com/skt/m/MathFP",
			SuperName: "java/lang/Object",
			Access:    jvm.AccessPublic | jvm.AccessFinal,
			Fields: []jvm.FieldDefinition{
				{Name: "E", Descriptor: "J", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessFinal, Constant: jvm.LongValue(2718281828)},
				{Name: "PI", Descriptor: "J", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessFinal, Constant: jvm.LongValue(3141592654)},
				{Name: "MAX_VALUE", Descriptor: "J", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessFinal, Constant: jvm.LongValue(9223372036854775807)},
				{Name: "MIN_VALUE", Descriptor: "J", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessFinal, Constant: jvm.LongValue(-9223372036854775808)},
			},
			Methods: []jvm.MethodDefinition{
				{Name: "abs", Descriptor: "(J)J", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "acos", Descriptor: "(J)J", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "add", Descriptor: "(JJ)J", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "asin", Descriptor: "(J)J", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "atan", Descriptor: "(J)J", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "cos", Descriptor: "(J)J", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "divide", Descriptor: "(JJ)J", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "exp", Descriptor: "(J)J", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "log", Descriptor: "(J)J", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "max", Descriptor: "(JJ)J", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "min", Descriptor: "(JJ)J", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "multiply", Descriptor: "(JJ)J", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "parseFP", Descriptor: "(J)J", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "parseFPString", Descriptor: "(Ljava/lang/String;)J", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "pow", Descriptor: "(JJ)J", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "round", Descriptor: "(J)J", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "sin", Descriptor: "(J)J", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "sqrt", Descriptor: "(J)J", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "sub", Descriptor: "(JJ)J", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "tan", Descriptor: "(J)J", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "toLong", Descriptor: "(J)J", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "toStringE", Descriptor: "(J)Ljava/lang/String;", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "toStringLF", Descriptor: "(JI)Ljava/lang/String;", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
			},
		},
		{
			Name:      "com/skt/m/PhoneBook",
			SuperName: "java/lang/Object",
			Access:    jvm.AccessPublic,
			Fields: []jvm.FieldDefinition{
				{Name: "FIELD_NAME", Descriptor: "I", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessFinal, Constant: jvm.IntValue(0)},
				{Name: "FIELD_NUMBER", Descriptor: "I", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessFinal, Constant: jvm.IntValue(1)},
			},
			Methods: []jvm.MethodDefinition{
				{Name: "first", Descriptor: "()V", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "next", Descriptor: "()I", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "findRecord", Descriptor: "(ILjava/lang/String;)V", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "getField", Descriptor: "(II)Ljava/lang/String;", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "getGroupNames", Descriptor: "()[Ljava/lang/String;", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "getMaxRecordID", Descriptor: "()I", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "getRecord", Descriptor: "(I)[Ljava/lang/String;", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "isUsed", Descriptor: "(I)Z", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
			},
		},
		{
			Name:      "com/skt/m/ProgressBar",
			SuperName: "java/lang/Object",
			Access:    jvm.AccessPublic,
			Methods: []jvm.MethodDefinition{
				{Name: "<init>", Descriptor: "(Ljava/lang/String;)V", Access: jvm.AccessPublic, Body: forwardInit(ProgressBarClass, "init", "(Ljava/lang/String;)V")},
				{Name: "init", Descriptor: "(Ljava/lang/String;)V", Access: jvm.AccessPrivate | jvm.AccessNative},
				{Name: "getValue", Descriptor: "()I", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "setValue", Descriptor: "(I)V", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "getMaxValue", Descriptor: "()I", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "setMaxValue", Descriptor: "(I)V", Access: jvm.AccessPublic | jvm.AccessNative},
			},
		},
		{
			Name:      "com/skt/m/ResourceAllocException",
			SuperName: "java/lang/Exception",
			Access:    jvm.AccessPublic,
			Methods: []jvm.MethodDefinition{
				{Name: "<init>", Descriptor: "()V", Access: jvm.AccessPublic, Body: exceptionInit("()V")},
				{Name: "<init>", Descriptor: "(Ljava/lang/String;)V", Access: jvm.AccessPublic, Body: exceptionInit("(Ljava/lang/String;)V")},
			},
		},
		{
			Name:      "com/skt/m/SISImage",
			SuperName: "java/lang/Object",
			Access:    jvm.AccessPublic,
			Methods: []jvm.MethodDefinition{
				{Name: "<init>", Descriptor: "()V", Access: jvm.AccessPublic, Body: emptyInit},
				{Name: "createBuffer", Descriptor: "(II)V", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "freeBuffer", Descriptor: "()V", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "getRequiredBufferSize", Descriptor: "([BII)I", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "getBestID", Descriptor: "()I", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "getDelay", Descriptor: "(I)I", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "getFrame", Descriptor: "(I)Ljavax/microedition/lcdui/Image;", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "getObject", Descriptor: "(IZ)Ljavax/microedition/lcdui/Image;", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "getWidth", Descriptor: "()I", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "getHeight", Descriptor: "()I", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "getImageLevel", Descriptor: "()I", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "getMaxFrameID", Descriptor: "()I", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "getMaxObjectID", Descriptor: "()I", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "paintFrame", Descriptor: "(Ljavax/microedition/lcdui/Graphics;III)V", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "paintObject", Descriptor: "(Ljavax/microedition/lcdui/Graphics;IIIZ)V", Access: jvm.AccessPublic | jvm.AccessNative},
			},
		},
		{
			Name:      "com/skt/m/SMS",
			SuperName: "java/lang/Object",
			Access:    jvm.AccessPublic,
			Methods: []jvm.MethodDefinition{
				{Name: "get", Descriptor: "(I)Lcom/skt/m/SMSMessage;", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "get", Descriptor: "(ILcom/skt/m/SMSMessage;)Z", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "send", Descriptor: "(Ljava/lang/String;Lcom/skt/m/SMSMessage;)Z", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "getSMSListener", Descriptor: "()Lcom/skt/m/SMSListener;", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "setSMSListener", Descriptor: "(Lcom/skt/m/SMSListener;)V", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
			},
		},
		{
			Name:      "com/skt/m/SMSListener",
			SuperName: "java/lang/Object",
			Access:    jvm.AccessPublic | jvm.AccessInterface | jvm.AccessAbstract,
			Methods: []jvm.MethodDefinition{
				{Name: "onMessage", Descriptor: "(Lcom/skt/m/SMSMessage;)V", Access: jvm.AccessPublic | jvm.AccessAbstract},
			},
		},
		{
			Name:      "com/skt/m/SMSMessage",
			SuperName: "java/lang/Object",
			Access:    jvm.AccessPublic,
			Fields: []jvm.FieldDefinition{
				{Name: "TYPE_TEXT", Descriptor: "I", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessFinal, Constant: jvm.IntValue(0)},
				{Name: "TYPE_CALLBACK", Descriptor: "I", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessFinal, Constant: jvm.IntValue(1)},
				{Name: "TYPE_URL", Descriptor: "I", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessFinal, Constant: jvm.IntValue(2)},
			},
			Methods: []jvm.MethodDefinition{
				{Name: "<init>", Descriptor: "()V", Access: jvm.AccessPublic, Body: smsMessageInit},
				{Name: "<init>", Descriptor: "([BLjava/lang/String;)V", Access: jvm.AccessPublic, Body: forwardInit(SMSMessageClass, "init", "([BLjava/lang/String;)V")},
				{Name: "init", Descriptor: "([BLjava/lang/String;)V", Access: jvm.AccessPrivate | jvm.AccessNative},
				{Name: "getShortMessage", Descriptor: "()[B", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "getAppData", Descriptor: "()[B", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "getSender", Descriptor: "()Ljava/lang/String;", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "getName", Descriptor: "()Ljava/lang/String;", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "getCName", Descriptor: "()Ljava/lang/String;", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "getComment", Descriptor: "()Ljava/lang/String;", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "getURL", Descriptor: "()Ljava/lang/String;", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "getServiceOption", Descriptor: "()B", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "getType", Descriptor: "()I", Access: jvm.AccessPublic | jvm.AccessNative},
			},
		},
		{
			Name:      "com/skt/m/UnsupportedFormatException",
			SuperName: "java/lang/Exception",
			Access:    jvm.AccessPublic,
			Methods: []jvm.MethodDefinition{
				{Name: "<init>", Descriptor: "()V", Access: jvm.AccessPublic, Body: exceptionInit("()V")},
				{Name: "<init>", Descriptor: "(Ljava/lang/String;)V", Access: jvm.AccessPublic, Body: exceptionInit("(Ljava/lang/String;)V")},
			},
		},
		{
			Name:      "com/skt/m/UserStopException",
			SuperName: "java/lang/Exception",
			Access:    jvm.AccessPublic,
			Methods: []jvm.MethodDefinition{
				{Name: "<init>", Descriptor: "()V", Access: jvm.AccessPublic, Body: exceptionInit("()V")},
				{Name: "<init>", Descriptor: "(Ljava/lang/String;)V", Access: jvm.AccessPublic, Body: exceptionInit("(Ljava/lang/String;)V")},
			},
		},
		{
			Name:      "com/skt/m/Vibration",
			SuperName: "java/lang/Object",
			Access:    jvm.AccessPublic,
			Methods: []jvm.MethodDefinition{
				{Name: "start", Descriptor: "(II)V", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "stop", Descriptor: "()V", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "isSupported", Descriptor: "()Z", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "getLevelNum", Descriptor: "()I", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
			},
		},
		{
			Name:      "com/skt/m3d/Graphics3D",
			SuperName: "java/lang/Object",
			Access:    jvm.AccessPublic,
			Methods: []jvm.MethodDefinition{
				{Name: "<init>", Descriptor: "()V", Access: jvm.AccessPublic, Body: emptyInit},
				{Name: "clearZBuffer", Descriptor: "()V", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "destroyZBuffer", Descriptor: "()V", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "isZBufferEnabled", Descriptor: "()Z", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "setZBufferEnabled", Descriptor: "(Z)V", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "isBackfaceCulled", Descriptor: "()Z", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "setBackfaceCulled", Descriptor: "(Z)V", Access: jvm.AccessPublic | jvm.AccessNative},
			},
		},
		{
			Name:      "com/skt/m3d/Object3D",
			SuperName: "java/lang/Object",
			Access:    jvm.AccessPublic,
			Methods: []jvm.MethodDefinition{
				{Name: "<init>", Descriptor: "(Ljava/lang/String;)V", Access: jvm.AccessPublic, Body: forwardInit(Object3DClass, "init", "(Ljava/lang/String;)V")},
				{Name: "init", Descriptor: "(Ljava/lang/String;)V", Access: jvm.AccessPrivate | jvm.AccessNative},
				{Name: "getName", Descriptor: "()Ljava/lang/String;", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "setName", Descriptor: "(Ljava/lang/String;)V", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "addVertex", Descriptor: "(III)V", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "addTriangle", Descriptor: "(IIII)V", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "setVertices", Descriptor: "([I[I[I)V", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "setTriangles", Descriptor: "([I[I[I[I)V", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "translate", Descriptor: "(III)V", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "rotate", Descriptor: "(III)V", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "scale", Descriptor: "(III)V", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "getMatrixRow0", Descriptor: "()[I", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "getMatrixRow1", Descriptor: "()[I", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "getMatrixRow2", Descriptor: "()[I", Access: jvm.AccessPublic | jvm.AccessNative},
			},
		},
		{
			Name:      "com/xce/io/FileInputStream",
			SuperName: "java/io/InputStream",
			Access:    jvm.AccessPublic,
			Fields: []jvm.FieldDefinition{
				{Name: "file", Descriptor: "Lcom/xce/io/XFile;", Access: jvm.AccessPrivate},
				{Name: "mark", Descriptor: "I", Access: jvm.AccessPrivate},
			},
			Methods: []jvm.MethodDefinition{
				{Name: "<init>", Descriptor: "(I)V", Access: jvm.AccessPublic, Body: openStreamFile(FileInputStreamClass, "(I)V", 0)},
				{Name: "<init>", Descriptor: "(Ljava/lang/String;)V", Access: jvm.AccessPublic, Throws: []string{"java/io/IOException"}, Body: openStreamFile(FileInputStreamClass, "(Ljava/lang/String;)V", xfileRead)},
				{Name: "<init>", Descriptor: "(Lcom/xce/io/XFile;)V", Access: jvm.AccessPublic, Body: openStreamFile(FileInputStreamClass, xfileDescriptor, 0)},
				{Name: "available", Descriptor: "()I", Access: jvm.AccessPublic, Body: fileDelegate(FileInputStreamClass, "available", "()I")},
				{Name: "close", Descriptor: "()V", Access: jvm.AccessPublic, Body: fileDelegate(FileInputStreamClass, "close", "()V")},
				{Name: "mark", Descriptor: "(I)V", Access: jvm.AccessPublic, Body: fileInputStreamMark},
				{Name: "markSupported", Descriptor: "()Z", Access: jvm.AccessPublic, Body: markIsSupported},
				{Name: "reset", Descriptor: "()V", Access: jvm.AccessPublic, Body: fileInputStreamReset},
				{Name: "read", Descriptor: "()I", Access: jvm.AccessPublic, Body: fileInputStreamRead},
				{Name: "read", Descriptor: "([B)I", Access: jvm.AccessPublic, Body: fileInputStreamReadArray},
				{Name: "read", Descriptor: "([BII)I", Access: jvm.AccessPublic, Body: fileInputStreamReadRange},
				{Name: "skip", Descriptor: "(J)J", Access: jvm.AccessPublic, Body: fileInputStreamSkip},
			},
		},
		{
			Name:      "com/xce/io/FileOutputStream",
			SuperName: "java/io/OutputStream",
			Access:    jvm.AccessPublic,
			Fields: []jvm.FieldDefinition{
				{Name: "file", Descriptor: "Lcom/xce/io/XFile;", Access: jvm.AccessPrivate},
			},
			Methods: []jvm.MethodDefinition{
				{Name: "<init>", Descriptor: "(I)V", Access: jvm.AccessPublic, Body: openStreamFile(FileOutputStreamClass, "(I)V", 0)},
				{Name: "<init>", Descriptor: "(Ljava/lang/String;)V", Access: jvm.AccessPublic, Throws: []string{"java/io/IOException"}, Body: openStreamFile(FileOutputStreamClass, "(Ljava/lang/String;)V", xfileWrite)},
				{Name: "<init>", Descriptor: "(Ljava/lang/String;Z)V", Access: jvm.AccessPublic, Throws: []string{"java/io/IOException"}, Body: fileOutputStreamAppendInit},
				{Name: "<init>", Descriptor: "(Lcom/xce/io/XFile;)V", Access: jvm.AccessPublic, Body: openStreamFile(FileOutputStreamClass, xfileDescriptor, 0)},
				{Name: "close", Descriptor: "()V", Access: jvm.AccessPublic, Body: fileDelegate(FileOutputStreamClass, "close", "()V")},
				{Name: "flush", Descriptor: "()V", Access: jvm.AccessPublic, Body: fileDelegate(FileOutputStreamClass, "flush", "()V")},
				{Name: "write", Descriptor: "(I)V", Access: jvm.AccessPublic, Body: fileOutputStreamWrite},
				{Name: "write", Descriptor: "([BII)V", Access: jvm.AccessPublic, Body: fileOutputStreamWriteRange},
			},
		},
		{
			Name:      "com/xce/io/XFile",
			SuperName: "java/lang/Object",
			Access:    jvm.AccessPublic,
			Fields: []jvm.FieldDefinition{
				{Name: "READ", Descriptor: "I", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessFinal, Constant: jvm.IntValue(1)},
				{Name: "WRITE", Descriptor: "I", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessFinal, Constant: jvm.IntValue(2)},
				{Name: "READ_WRITE", Descriptor: "I", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessFinal, Constant: jvm.IntValue(3)},
				{Name: "READ_DIRECTORY", Descriptor: "I", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessFinal, Constant: jvm.IntValue(4)},
				{Name: "READ_RESOURCE", Descriptor: "I", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessFinal, Constant: jvm.IntValue(8)},
				{Name: "SEEK_SET", Descriptor: "I", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessFinal, Constant: jvm.IntValue(0)},
				{Name: "SEEK_CUR", Descriptor: "I", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessFinal, Constant: jvm.IntValue(1)},
				{Name: "SEEK_END", Descriptor: "I", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessFinal, Constant: jvm.IntValue(2)},
			},
			Methods: []jvm.MethodDefinition{
				{Name: "<init>", Descriptor: "(I)V", Access: jvm.AccessPublic, Body: xfileInitHandle},
				{Name: "<init>", Descriptor: "(Ljava/lang/String;I)V", Access: jvm.AccessPublic, Throws: []string{"java/io/IOException"}, Body: xfileInitName},
				{Name: "<init>", Descriptor: "(Ljava/lang/String;Ljava/lang/String;)V", Access: jvm.AccessPublic, Throws: []string{"java/io/IOException"}, Body: xfileInitMode},
				{Name: "initHandle", Descriptor: "(I)V", Access: jvm.AccessPrivate | jvm.AccessNative},
				{Name: "initName", Descriptor: "(Ljava/lang/String;I)V", Access: jvm.AccessPrivate | jvm.AccessNative, Throws: []string{"java/io/IOException"}},
				{Name: "available", Descriptor: "()I", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "close", Descriptor: "()V", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "flush", Descriptor: "()V", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "read", Descriptor: "([BII)I", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "write", Descriptor: "([BII)I", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "seek", Descriptor: "(II)I", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "readdir", Descriptor: "()Ljava/lang/String;", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "exists", Descriptor: "(Ljava/lang/String;)Z", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "filesize", Descriptor: "(Ljava/lang/String;)I", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "fsavail", Descriptor: "()I", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "fsused", Descriptor: "()I", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "mkdir", Descriptor: "(Ljava/lang/String;)V", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "rmdir", Descriptor: "(Ljava/lang/String;)V", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "rmrdir", Descriptor: "(Ljava/lang/String;)V", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "unlink", Descriptor: "(Ljava/lang/String;)I", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
			},
		},
		{
			// The vendor's decoder pair, which is the java.io converter API of
			// the day: convert fills a char array from a byte array and
			// answers how many characters it wrote, and flush empties whatever
			// a partial character left behind. A title reads its own Korean
			// text through this rather than through String, because it wants
			// the characters in a buffer it already owns.
			Name:      ByteToCharConverterClass,
			SuperName: "java/lang/Object",
			Access:    jvm.AccessPublic | jvm.AccessAbstract,
			Methods: []jvm.MethodDefinition{
				{Name: "<init>", Descriptor: "()V", Access: jvm.AccessProtected, Body: emptyInit},
				{Name: "convert", Descriptor: "([BII[CII)I", Access: jvm.AccessPublic | jvm.AccessAbstract},
				{Name: "flush", Descriptor: "([CII)I", Access: jvm.AccessPublic | jvm.AccessAbstract},
			},
		},
		{
			Name:      ByteToCharEUCKRClass,
			SuperName: ByteToCharConverterClass,
			Access:    jvm.AccessPublic,
			Fields: []jvm.FieldDefinition{
				// The byte a converted range ended on when it was the first
				// half of a character. The next convert starts with it.
				{Name: "pending", Descriptor: "I", Access: jvm.AccessPrivate},
			},
			Methods: []jvm.MethodDefinition{
				{Name: "<init>", Descriptor: "()V", Access: jvm.AccessPublic, Body: emptyInit},
				{Name: "convert", Descriptor: "([BII[CII)I", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "flush", Descriptor: "([CII)I", Access: jvm.AccessPublic | jvm.AccessNative},
			},
		},
		{
			Name:      "com/xce/lcdui/Toolkit",
			SuperName: "java/lang/Object",
			Access:    jvm.AccessPublic,
			// The vendor's toolkit publishes the metrics of the one font a
			// handset drew everything with, and the screen Graphics beside
			// them. Ten local titles read one of these fields, and they read
			// them rather than call anything: a field a class initializer never
			// filled is a zero, and a title that lays a menu out on a font
			// height of zero draws every line on top of the last.
			Fields: []jvm.FieldDefinition{
				{Name: "DEFAULT_FONT", Descriptor: "Ljavax/microedition/lcdui/Font;", Access: jvm.AccessPublic | jvm.AccessStatic},
				{Name: "FONT_HEIGHT", Descriptor: "I", Access: jvm.AccessPublic | jvm.AccessStatic},
				{Name: "FONT_GAP", Descriptor: "I", Access: jvm.AccessPublic | jvm.AccessStatic},
				{Name: "MAX_CHARWIDTH", Descriptor: "I", Access: jvm.AccessPublic | jvm.AccessStatic},
				{Name: "graphics", Descriptor: "Ljavax/microedition/lcdui/Graphics;", Access: jvm.AccessPublic | jvm.AccessStatic},
				{Name: "BLACK", Descriptor: "I", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessFinal, Constant: jvm.IntValue(0x000000)},
				{Name: "DK_GRAY", Descriptor: "I", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessFinal, Constant: jvm.IntValue(0x555555)},
				{Name: "LT_GRAY", Descriptor: "I", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessFinal, Constant: jvm.IntValue(0xaaaaaa)},
				{Name: "WHITE", Descriptor: "I", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessFinal, Constant: jvm.IntValue(0xffffff)},
			},
			Methods: []jvm.MethodDefinition{
				{Name: "<init>", Descriptor: "()V", Access: jvm.AccessPublic, Body: emptyInit},
				{Name: "<clinit>", Descriptor: "()V", Access: jvm.AccessStatic, Body: toolkitClassInit},
				{Name: "drawString", Descriptor: "(Ljava/lang/String;III)V", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "screenGraphics", Descriptor: "()Ljavax/microedition/lcdui/Graphics;", Access: jvm.AccessPrivate | jvm.AccessStatic | jvm.AccessNative},
				{Name: "getScreenWidth", Descriptor: "()I", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "getScreenHeight", Descriptor: "()I", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
			},
		},
		{
			Name:      "com/xce/lcdui/XDisplay",
			SuperName: "java/lang/Object",
			Access:    jvm.AccessPublic,
			Fields: []jvm.FieldDefinition{
				{Name: "width", Descriptor: "I", Access: jvm.AccessPublic | jvm.AccessStatic},
				{Name: "height", Descriptor: "I", Access: jvm.AccessPublic | jvm.AccessStatic},
				{Name: "height2", Descriptor: "I", Access: jvm.AccessPublic | jvm.AccessStatic},
			},
			Methods: []jvm.MethodDefinition{
				{Name: "refresh", Descriptor: "(IIII)V", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "drawImageEx", Descriptor: "(Ljavax/microedition/lcdui/Graphics;Ljavax/microedition/lcdui/Image;IILjavax/microedition/lcdui/Image;IIIII)V", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "copyLCD", Descriptor: "(Ljavax/microedition/lcdui/Graphics;Ljavax/microedition/lcdui/Image;IIII)V", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
			},
		},
		{
			// The vendor's own text component: the interface a title
			// implements when it keeps the text itself, and the handler the
			// platform's input method drives it through. Two classes in one
			// local title implement the interface, and its members are the
			// thirteen both of them carry.
			Name:      TextComponentClass,
			SuperName: "java/lang/Object",
			Access:    jvm.AccessPublic | jvm.AccessInterface | jvm.AccessAbstract,
			Methods: []jvm.MethodDefinition{
				{Name: "getCaretPosition", Descriptor: "()I", Access: jvm.AccessPublic | jvm.AccessAbstract},
				{Name: "getConstraints", Descriptor: "()I", Access: jvm.AccessPublic | jvm.AccessAbstract},
				{Name: "getMaxSize", Descriptor: "()I", Access: jvm.AccessPublic | jvm.AccessAbstract},
				{Name: "size", Descriptor: "()I", Access: jvm.AccessPublic | jvm.AccessAbstract},
				{Name: "insert", Descriptor: "(C)V", Access: jvm.AccessPublic | jvm.AccessAbstract},
				{Name: "delete", Descriptor: "()V", Access: jvm.AccessPublic | jvm.AccessAbstract},
				{Name: "clear", Descriptor: "()V", Access: jvm.AccessPublic | jvm.AccessAbstract},
				{Name: "replace", Descriptor: "(C)V", Access: jvm.AccessPublic | jvm.AccessAbstract},
				{Name: "moveCursor", Descriptor: "(I)V", Access: jvm.AccessPublic | jvm.AccessAbstract},
				{Name: "setCaretPosition", Descriptor: "(I)V", Access: jvm.AccessPublic | jvm.AccessAbstract},
				{Name: "setCaretVisible", Descriptor: "(Z)V", Access: jvm.AccessPublic | jvm.AccessAbstract},
				{Name: "repaint", Descriptor: "()V", Access: jvm.AccessPublic | jvm.AccessAbstract},
				{Name: "repaintIM", Descriptor: "()V", Access: jvm.AccessPublic | jvm.AccessAbstract},
			},
		},
		{
			Name:      TextComponentHandlerClass,
			SuperName: "java/lang/Object",
			Access:    jvm.AccessPublic | jvm.AccessFinal,
			Methods: []jvm.MethodDefinition{
				// The handler is the platform's, so a title reaches it
				// through the static rather than by making one.
				{Name: "<init>", Descriptor: "()V", Access: jvm.AccessPrivate, Body: emptyInit},
				{Name: "getTextComponentHandler", Descriptor: "()L" + TextComponentHandlerClass + ";", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessNative},
				{Name: "setTextComponent", Descriptor: "(L" + TextComponentClass + ";)V", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "getInputMode", Descriptor: "()I", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "clear", Descriptor: "()V", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "keyPressed", Descriptor: "(I)Z", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "keyReleased", Descriptor: "(I)Z", Access: jvm.AccessPublic | jvm.AccessNative},
			},
		},
		{
			Name:      "com/xce/lcdui/XTextField",
			SuperName: "java/lang/Object",
			Access:    jvm.AccessPublic,
			Methods: []jvm.MethodDefinition{
				{Name: "<init>", Descriptor: "()V", Access: jvm.AccessPublic, Body: forwardInit(XTextFieldClass, "init", "()V")},
				// The four-argument form is the one a title with a name
				// screen uses: the text it starts with, how many characters
				// it takes, the constraints a MIDP TextField would take, and
				// the Canvas it is painted on.
				{Name: "<init>", Descriptor: "(Ljava/lang/String;IILjavax/microedition/lcdui/Canvas;)V", Access: jvm.AccessPublic,
					Body: forwardInit(XTextFieldClass, "init", "(Ljava/lang/String;IILjavax/microedition/lcdui/Canvas;)V")},
				{Name: "init", Descriptor: "()V", Access: jvm.AccessPrivate | jvm.AccessNative},
				{Name: "init", Descriptor: "(Ljava/lang/String;IILjavax/microedition/lcdui/Canvas;)V", Access: jvm.AccessPrivate | jvm.AccessNative},
				{Name: "getText", Descriptor: "()Ljava/lang/String;", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "setText", Descriptor: "(Ljava/lang/String;)V", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "getMaxSize", Descriptor: "()I", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "setMaxSize", Descriptor: "(I)V", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "hasFocus", Descriptor: "()Z", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "setFocus", Descriptor: "(Z)V", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "setBounds", Descriptor: "(IIII)V", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "inputChar", Descriptor: "(C)V", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "keyPressed", Descriptor: "(I)V", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "keyReleased", Descriptor: "(I)V", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "keyRepeated", Descriptor: "(I)V", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "paint", Descriptor: "(Ljavax/microedition/lcdui/Graphics;)V", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "repaint", Descriptor: "()V", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "repaint", Descriptor: "(IIII)V", Access: jvm.AccessPublic | jvm.AccessNative},
			},
		},
		{
			Name:       "net/wfeature/RuntimeAudioClip",
			SuperName:  "java/lang/Object",
			Interfaces: []string{"com/skt/m/AudioClip"},
			Access:     jvm.AccessPublic,
			Fields: []jvm.FieldDefinition{
				{Name: "handle", Descriptor: "I", Access: jvm.AccessPrivate},
				{Name: "type", Descriptor: "Ljava/lang/String;", Access: jvm.AccessPrivate},
			},
			Methods: []jvm.MethodDefinition{
				{Name: "<init>", Descriptor: "(Ljava/lang/String;)V", Access: jvm.AccessPublic, Body: runtimeAudioClipInit},
				{Name: "open", Descriptor: "([BII)V", Access: jvm.AccessPublic | jvm.AccessNative, Throws: []string{"com/skt/m/UnsupportedFormatException"}},
				{Name: "close", Descriptor: "()V", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "play", Descriptor: "()V", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "loop", Descriptor: "()V", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "pause", Descriptor: "()V", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "resume", Descriptor: "()V", Access: jvm.AccessPublic | jvm.AccessNative},
				{Name: "stop", Descriptor: "()V", Access: jvm.AccessPublic | jvm.AccessNative},
			},
		},
	}...)
}
