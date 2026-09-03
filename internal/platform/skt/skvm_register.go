package skt

import (
	"fmt"
	"math"

	"github.com/movingwoo/wfeature/internal/api/skvm"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// registerSKVMNatives connects the SKVM class surface — com.skt.m, com.skt.m3d
// and com.xce — to this runtime. It runs only for the SKT platform; the
// classes are not in a plain MIDlet's world.
func (runtime *Runtime) registerSKVMNatives() error {
	const (
		text     = "Ljava/lang/String;"
		image    = "Ljavax/microedition/lcdui/Image;"
		graphics = "Ljavax/microedition/lcdui/Graphics;"
	)

	registrations := []nativeRegistration{
		{skvm.MathFPClass, "abs", "(J)J", mathFPAbs},
		{skvm.MathFPClass, "add", "(JJ)J", mathFPAdd},
		{skvm.MathFPClass, "sub", "(JJ)J", mathFPSub},
		{skvm.MathFPClass, "multiply", "(JJ)J", mathFPBinary(func(a, b float64) float64 { return a * b })},
		{skvm.MathFPClass, "divide", "(JJ)J", mathFPDivide},
		{skvm.MathFPClass, "max", "(JJ)J", mathFPBinary(math.Max)},
		{skvm.MathFPClass, "min", "(JJ)J", mathFPBinary(math.Min)},
		{skvm.MathFPClass, "pow", "(JJ)J", mathFPBinary(math.Pow)},
		{skvm.MathFPClass, "sin", "(J)J", mathFPUnary(math.Sin)},
		{skvm.MathFPClass, "cos", "(J)J", mathFPUnary(math.Cos)},
		{skvm.MathFPClass, "tan", "(J)J", mathFPUnary(math.Tan)},
		{skvm.MathFPClass, "asin", "(J)J", mathFPUnary(math.Asin)},
		{skvm.MathFPClass, "acos", "(J)J", mathFPUnary(math.Acos)},
		{skvm.MathFPClass, "atan", "(J)J", mathFPUnary(math.Atan)},
		{skvm.MathFPClass, "exp", "(J)J", mathFPUnary(math.Exp)},
		{skvm.MathFPClass, "log", "(J)J", mathFPUnary(math.Log)},
		{skvm.MathFPClass, "sqrt", "(J)J", mathFPUnary(math.Sqrt)},
		{skvm.MathFPClass, "round", "(J)J", mathFPRound},
		{skvm.MathFPClass, "toLong", "(J)J", mathFPToLong},
		{skvm.MathFPClass, "parseFP", "(J)J", mathFPParse},
		{skvm.MathFPClass, "parseFPString", "(" + text + ")J", runtime.mathFPParseString},
		{skvm.MathFPClass, "toStringE", "(J)" + text, runtime.mathFPToStringE},
		{skvm.MathFPClass, "toStringLF", "(JI)" + text, runtime.mathFPToStringLF},

		{skvm.Graphics2DClass, "init", "(" + graphics + ")V", runtime.initGraphics2D},
		{skvm.Graphics2DClass, "getPixel", "(II)I", runtime.graphics2DPixel},
		{skvm.Graphics2DClass, "setPixel", "(III)V", runtime.setGraphics2DPixel},
		{skvm.Graphics2DClass, "getPixelMask", "(II)Z", runtime.graphics2DPixelMask},
		{skvm.Graphics2DClass, "setPixelMask", "(IIZ)V", runtime.setGraphics2DPixelMask},
		{skvm.Graphics2DClass, "invertRect", "(IIII)V", runtime.graphics2DInvertRect},
		{skvm.Graphics2DClass, "captureLCD", "(IIII)" + image, runtime.graphics2DCaptureLCD},
		{skvm.Graphics2DClass, "drawImage", "(II" + image + "IIIII)V", runtime.graphics2DDrawImage},

		{skvm.BackLightClass, "on", "(I)V", runtime.backLightOn},
		{skvm.BackLightClass, "off", "()V", runtime.backLightOff},
		{skvm.BackLightClass, "getColor", "()I", runtime.backLightColor},
		{skvm.BackLightClass, "setColor", "(I)V", runtime.setBackLightColor},

		{skvm.VibrationClass, "start", "(II)V", runtime.vibrationStart},
		{skvm.VibrationClass, "stop", "()V", runtime.vibrationStop},
		{skvm.ToolkitClass, "screenGraphics", "()Ljavax/microedition/lcdui/Graphics;", runtime.toolkitScreenGraphics},
		{skvm.ByteToCharEUCKRClass, "convert", "([BII[CII)I", runtime.byteToCharConvert},
		{skvm.ByteToCharEUCKRClass, "flush", "([CII)I", runtime.byteToCharFlush},
		{skvm.VibrationClass, "isSupported", "()Z", runtime.vibrationSupported},
		{skvm.VibrationClass, "getLevelNum", "()I", runtime.vibrationLevels},

		{skvm.DeviceClass, "beep", "(II)V", runtime.deviceBeep},
		{skvm.DeviceClass, "setNAI", "(I)V", runtime.deviceSetNAI},
		{skvm.DeviceClass, "setBacklightEnabled", "(Z)V", runtime.setDeviceBacklight},
		{skvm.DeviceClass, "isBacklightEnabled", "()Z", runtime.deviceBacklightEnabled},
		{skvm.DeviceClass, "setKeyToneEnabled", "(Z)V", runtime.setDeviceKeyTone},
		{skvm.DeviceClass, "isKeyToneEnabled", "()Z", runtime.deviceKeyToneEnabled},
		{skvm.DeviceClass, "setColorMode", "(I)V", deviceAccepted(1)},
		{skvm.DeviceClass, "enableRestoreLCD", "(Z)V", deviceAccepted(1)},
		{skvm.DeviceClass, "setKeyRepeatTime", "(II)V", deviceAccepted(2)},
		{skvm.DeviceClass, "invokeWapBrowser", "(" + text + ")V", runtime.deviceInvokeWapBrowser},
		{skvm.DeviceClass, "setSISImage", "(I" + text + "[B)Z", runtime.deviceInstallRefused},
		{skvm.DeviceClass, "setMelody", "(I" + text + "[B)Z", runtime.deviceInstallRefused},

		{skvm.ProgressBarClass, "init", "(" + text + ")V", runtime.initProgressBar},
		{skvm.ProgressBarClass, "getValue", "()I", runtime.progressBarValue},
		{skvm.ProgressBarClass, "setValue", "(I)V", runtime.setProgressBarValue},
		{skvm.ProgressBarClass, "getMaxValue", "()I", runtime.progressBarMaxValue},
		{skvm.ProgressBarClass, "setMaxValue", "(I)V", runtime.setProgressBarMaxValue},

		{skvm.AudioSystemClass, "getAudioClip", "(" + text + ")Lcom/skt/m/AudioClip;", runtime.getAudioClip},
		{skvm.AudioSystemClass, "getVolume", "()I", runtime.audioVolume},
		{skvm.AudioSystemClass, "setVolume", "(I)V", runtime.setAudioVolume},
		{skvm.AudioSystemClass, "getMaxVolume", "(" + text + ")I", runtime.maxAudioVolume},
		{skvm.AudioSystemClass, "getVolume", "(" + text + ")I", runtime.audioVolumeForFormat},
		{skvm.AudioSystemClass, "setVolume", "(" + text + "I)V", runtime.setAudioVolumeForFormat},

		{skvm.RuntimeAudioClipClass, "open", "([BII)V", runtime.audioClipOpen},
		{skvm.RuntimeAudioClipClass, "close", "()V", runtime.audioClipAction("close")},
		{skvm.RuntimeAudioClipClass, "loop", "()V", runtime.audioClipAction("loop")},
		{skvm.RuntimeAudioClipClass, "pause", "()V", runtime.audioClipAction("pause")},
		{skvm.RuntimeAudioClipClass, "resume", "()V", runtime.audioClipAction("resume")},
		{skvm.RuntimeAudioClipClass, "stop", "()V", runtime.audioClipAction("stop")},

		{skvm.SMSClass, "get", "(I)Lcom/skt/m/SMSMessage;", runtime.smsGet},
		{skvm.SMSClass, "get", "(ILcom/skt/m/SMSMessage;)Z", runtime.smsGetInto},
		{skvm.SMSClass, "send", "(" + text + "Lcom/skt/m/SMSMessage;)Z", runtime.smsSend},
		{skvm.SMSClass, "getSMSListener", "()Lcom/skt/m/SMSListener;", runtime.smsListener},
		{skvm.SMSClass, "setSMSListener", "(Lcom/skt/m/SMSListener;)V", runtime.setSMSListener},

		{skvm.SMSMessageClass, "init", "([B" + text + ")V", runtime.initSMSMessage},
		{skvm.SMSMessageClass, "getShortMessage", "()[B", runtime.smsShortMessage},
		{skvm.SMSMessageClass, "getAppData", "()[B", nullReference},
		{skvm.SMSMessageClass, "getSender", "()" + text, runtime.smsSender},
		{skvm.SMSMessageClass, "getName", "()" + text, nullReference},
		{skvm.SMSMessageClass, "getCName", "()" + text, nullReference},
		{skvm.SMSMessageClass, "getComment", "()" + text, nullReference},
		{skvm.SMSMessageClass, "getURL", "()" + text, nullReference},
		{skvm.SMSMessageClass, "getServiceOption", "()B", zeroByte},
		{skvm.SMSMessageClass, "getType", "()I", zeroInt},

		{skvm.CallClass, "call", "(" + text + ")Z", runtime.smsSend},

		{skvm.PhoneBookClass, "first", "()V", runtime.ignoreVoid},
		{skvm.PhoneBookClass, "next", "()I", func(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
			// -1 is "no more records", which is what an empty book reports.
			return jvm.IntValue(-1), nil
		}},
		{skvm.PhoneBookClass, "findRecord", "(I" + text + ")V", runtime.ignoreVoid},
		{skvm.PhoneBookClass, "getField", "(II)" + text, nullReference},
		{skvm.PhoneBookClass, "getGroupNames", "()[" + text, runtime.emptyStringArray},
		{skvm.PhoneBookClass, "getMaxRecordID", "()I", zeroInt},
		{skvm.PhoneBookClass, "getRecord", "(I)[" + text, nullReference},
		{skvm.PhoneBookClass, "isUsed", "(I)Z", zeroInt},

		{skvm.XDisplayClass, "refresh", "(IIII)V", runtime.xDisplayRefresh},
		{skvm.XDisplayClass, "drawImageEx", "(" + graphics + image + "II" + image + "IIIII)V", runtime.xDisplayDrawImageEx},
		{skvm.XDisplayClass, "copyLCD", "(" + graphics + image + "IIII)V", runtime.xDisplayCopyLCD},
		{skvm.ToolkitClass, "drawString", "(" + text + "III)V", runtime.toolkitDrawString},
		{skvm.ToolkitClass, "getScreenWidth", "()I", runtime.toolkitScreenWidth},
		{skvm.ToolkitClass, "getScreenHeight", "()I", runtime.toolkitScreenHeight},

		{skvm.TextComponentHandlerClass, "getTextComponentHandler", "()L" + skvm.TextComponentHandlerClass + ";", runtime.textComponentHandler},
		{skvm.TextComponentHandlerClass, "setTextComponent", "(L" + skvm.TextComponentClass + ";)V", runtime.setTextComponent},
		{skvm.TextComponentHandlerClass, "getInputMode", "()I", runtime.textComponentInputMode},
		{skvm.TextComponentHandlerClass, "clear", "()V", runtime.clearTextComponentHandler},
		{skvm.TextComponentHandlerClass, "keyPressed", "(I)Z", runtime.textComponentKeyPressed},
		{skvm.TextComponentHandlerClass, "keyReleased", "(I)Z", runtime.textComponentKeyReleased},

		{skvm.XTextFieldClass, "init", "()V", runtime.initXTextField},
		{skvm.XTextFieldClass, "init", "(" + text + "IILjavax/microedition/lcdui/Canvas;)V", runtime.initXTextFieldWithText},
		{skvm.XTextFieldClass, "getText", "()" + text, runtime.xTextFieldText},
		{skvm.XTextFieldClass, "setText", "(" + text + ")V", runtime.setXTextFieldText},
		{skvm.XTextFieldClass, "getMaxSize", "()I", runtime.xTextFieldMaxSize},
		{skvm.XTextFieldClass, "setMaxSize", "(I)V", runtime.setXTextFieldMaxSize},
		{skvm.XTextFieldClass, "hasFocus", "()Z", runtime.xTextFieldFocus},
		{skvm.XTextFieldClass, "setFocus", "(Z)V", runtime.setXTextFieldFocus},
		{skvm.XTextFieldClass, "setBounds", "(IIII)V", runtime.setXTextFieldBounds},
		{skvm.XTextFieldClass, "inputChar", "(C)V", runtime.xTextFieldInputChar},
		{skvm.XTextFieldClass, "keyPressed", "(I)V", runtime.xTextFieldKeyPressed},
		{skvm.XTextFieldClass, "keyReleased", "(I)V", runtime.ignoreVoid},
		{skvm.XTextFieldClass, "keyRepeated", "(I)V", runtime.ignoreVoid},
		{skvm.XTextFieldClass, "paint", "(" + graphics + ")V", runtime.xTextFieldPaint},
		{skvm.XTextFieldClass, "repaint", "()V", runtime.ignoreVoid},
		{skvm.XTextFieldClass, "repaint", "(IIII)V", runtime.ignoreVoid},

		{skvm.Object3DClass, "init", "(" + text + ")V", runtime.initObject3D},
		{skvm.Object3DClass, "getName", "()" + text, runtime.object3DName},
		{skvm.Object3DClass, "setName", "(" + text + ")V", runtime.setObject3DName},
		{skvm.Object3DClass, "addVertex", "(III)V", runtime.object3DAddVertex},
		{skvm.Object3DClass, "addTriangle", "(IIII)V", runtime.object3DAddTriangle},
		{skvm.Object3DClass, "setVertices", "([I[I[I)V", runtime.object3DSetVertices},
		{skvm.Object3DClass, "setTriangles", "([I[I[I[I)V", runtime.object3DSetTriangles},
		{skvm.Object3DClass, "translate", "(III)V", runtime.object3DTranslate},
		{skvm.Object3DClass, "rotate", "(III)V", runtime.object3DRotate},
		{skvm.Object3DClass, "scale", "(III)V", runtime.object3DScale},
		{skvm.Object3DClass, "getMatrixRow0", "()[I", runtime.object3DMatrixRow(0)},
		{skvm.Object3DClass, "getMatrixRow1", "()[I", runtime.object3DMatrixRow(1)},
		{skvm.Object3DClass, "getMatrixRow2", "()[I", runtime.object3DMatrixRow(2)},

		{skvm.Graphics3DClass, "clearZBuffer", "()V", runtime.ignoreVoid},
		{skvm.Graphics3DClass, "destroyZBuffer", "()V", runtime.ignoreVoid},
		{skvm.Graphics3DClass, "isZBufferEnabled", "()Z",
			runtime.graphics3DFlag(true, func(state *skvmState) *bool { return &state.zBufferEnabled })},
		{skvm.Graphics3DClass, "setZBufferEnabled", "(Z)V",
			runtime.graphics3DFlag(false, func(state *skvmState) *bool { return &state.zBufferEnabled })},
		{skvm.Graphics3DClass, "isBackfaceCulled", "()Z",
			runtime.graphics3DFlag(true, func(state *skvmState) *bool { return &state.backfaceCulling })},
		{skvm.Graphics3DClass, "setBackfaceCulled", "(Z)V",
			runtime.graphics3DFlag(false, func(state *skvmState) *bool { return &state.backfaceCulling })},

		{skvm.SISImageClass, "createBuffer", "(II)V", runtime.sisCreateBuffer},
		{skvm.SISImageClass, "freeBuffer", "()V", runtime.ignoreVoid},
		{skvm.SISImageClass, "getRequiredBufferSize", "([BII)I", runtime.sisRequiredBufferSize},
		{skvm.SISImageClass, "getBestID", "()I", zeroInt},
		{skvm.SISImageClass, "getDelay", "(I)I", zeroInt},
		{skvm.SISImageClass, "getFrame", "(I)" + image, runtime.sisFrame},
		{skvm.SISImageClass, "getObject", "(IZ)" + image, runtime.sisFrame},
		{skvm.SISImageClass, "getWidth", "()I", runtime.sisDimension(func(data *sisImageData) int32 { return data.width })},
		{skvm.SISImageClass, "getHeight", "()I", runtime.sisDimension(func(data *sisImageData) int32 { return data.height })},
		{skvm.SISImageClass, "getImageLevel", "()I", zeroInt},
		{skvm.SISImageClass, "getMaxFrameID", "()I", zeroInt},
		{skvm.SISImageClass, "getMaxObjectID", "()I", zeroInt},
		{skvm.SISImageClass, "paintFrame", "(" + graphics + "III)V", runtime.ignoreVoid},
		{skvm.SISImageClass, "paintObject", "(" + graphics + "IIIZ)V", runtime.ignoreVoid},
	}

	registrations = append(registrations, runtime.xFileRegistrations()...)

	// `play` is the one native here that has to know which thread called it,
	// because it waits — see audioClipPlay.
	if err := runtime.registerContextNative(skvm.RuntimeAudioClipClass, "play", "()V", runtime.audioClipPlay); err != nil {
		return fmt.Errorf("register %s.play()V: %w", skvm.RuntimeAudioClipClass, err)
	}

	for _, registration := range registrations {
		if err := runtime.registerNative(registration.class, registration.name, registration.descriptor, registration.method); err != nil {
			return fmt.Errorf("register %s.%s%s: %w", registration.class, registration.name, registration.descriptor, err)
		}
	}
	return runtime.publishScreenSize()
}

// publishScreenSize fills in the screen XDisplay exposes as static fields.
// Games read them the way a MIDlet reads Canvas.getWidth, and they read them
// early: a common frame loop caches them in a Canvas constructor and repaints
// that rectangle forever after, so a zero here is a title that runs, paints
// nothing, and reports no error at all.
//
// height2 is the drawable height on a handset that reserved rows for a soft-key
// bar. This runtime gives a Canvas the whole framebuffer, so both heights are
// the framebuffer's.
func (runtime *Runtime) publishScreenSize() error {
	fields := []struct {
		name  string
		value int32
	}{
		{"width", int32(runtime.frameWidth)},
		{"height", int32(runtime.frameHeight)},
		{"height2", int32(runtime.frameHeight)},
	}
	for _, field := range fields {
		if err := runtime.VM.SetStaticField(skvm.XDisplayClass, field.name, "I", jvm.IntValue(field.value)); err != nil {
			return fmt.Errorf("publish %s.%s: %w", skvm.XDisplayClass, field.name, err)
		}
	}
	return nil
}
