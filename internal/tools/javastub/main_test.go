package main

import (
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// The stub is what javac reads, so what matters is that a fixture can say
// everything the runtime allows: extend the class, override the method,
// declare the exception, and read the constant.
func TestSourceRendersACompilableShape(t *testing.T) {
	rendered := source(jvm.ClassDefinition{
		Name:       "javax/microedition/lcdui/Example",
		SuperName:  "javax/microedition/lcdui/Displayable",
		Interfaces: []string{"java/lang/Runnable"},
		Access:     jvm.AccessPublic | jvm.AccessAbstract,
		Fields: []jvm.FieldDefinition{
			{Name: "LIMIT", Descriptor: "I", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessFinal, Constant: jvm.IntValue(7)},
			{Name: "NAME", Descriptor: "Ljava/lang/String;", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessFinal, Constant: jvm.StringValue("example")},
			{Name: "MADE", Descriptor: "Ljavax/microedition/lcdui/Example;", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessFinal},
			{Name: "hidden", Descriptor: "I", Access: jvm.AccessPrivate},
		},
		Methods: []jvm.MethodDefinition{
			{Name: "<init>", Descriptor: "()V", Access: jvm.AccessProtected},
			{Name: "read", Descriptor: "([BII)I", Access: jvm.AccessPublic, Throws: []string{"java/io/IOException"}},
			{Name: "run", Descriptor: "()V", Access: jvm.AccessPublic | jvm.AccessAbstract},
			{Name: "of", Descriptor: "(Ljava/lang/String;)Ljavax/microedition/lcdui/Example;", Access: jvm.AccessPublic | jvm.AccessStatic},
			{Name: "secret", Descriptor: "()V", Access: jvm.AccessPrivate},
			{Name: "<clinit>", Descriptor: "()V", Access: jvm.AccessStatic},
		},
	})

	wanted := []string{
		"package javax.microedition.lcdui;",
		"public abstract class Example extends javax.microedition.lcdui.Displayable implements java.lang.Runnable {",
		"public static final int LIMIT = 7;",
		`public static final java.lang.String NAME = "example";`,
		"public static final javax.microedition.lcdui.Example MADE = null;",
		"protected Example() {",
		"public native int read(byte[] argument0, int argument1, int argument2) throws java.io.IOException;",
		"public abstract void run();",
		"public static native javax.microedition.lcdui.Example of(java.lang.String argument0);",
	}
	for _, want := range wanted {
		if !strings.Contains(rendered, want) {
			t.Errorf("stub is missing %q:\n%s", want, rendered)
		}
	}

	// A private member is not part of the surface, and the class initializer
	// is the runtime's rather than something a fixture can name.
	for _, unwanted := range []string{"hidden", "secret", "clinit"} {
		if strings.Contains(rendered, unwanted) {
			t.Errorf("stub exposes %q:\n%s", unwanted, rendered)
		}
	}
}

func TestInterfaceStubOmitsImpliedModifiers(t *testing.T) {
	rendered := source(jvm.ClassDefinition{
		Name:       "javax/microedition/rms/RecordFilter",
		SuperName:  "java/lang/Object",
		Access:     jvm.AccessPublic | jvm.AccessInterface | jvm.AccessAbstract,
		Fields:     []jvm.FieldDefinition{{Name: "ALL", Descriptor: "I", Access: jvm.AccessPublic | jvm.AccessStatic | jvm.AccessFinal, Constant: jvm.IntValue(1)}},
		Methods:    []jvm.MethodDefinition{{Name: "matches", Descriptor: "([B)Z", Access: jvm.AccessPublic | jvm.AccessAbstract}},
		Interfaces: nil,
	})
	if !strings.Contains(rendered, "public interface RecordFilter {") {
		t.Errorf("interface stub is wrong:\n%s", rendered)
	}
	if !strings.Contains(rendered, "boolean matches(byte[] argument0);") {
		t.Errorf("interface method is wrong:\n%s", rendered)
	}
	if strings.Contains(rendered, "native") || strings.Contains(rendered, "abstract boolean") {
		t.Errorf("interface stub carries modifiers javac rejects there:\n%s", rendered)
	}
	if !strings.Contains(rendered, "int ALL = 1;") {
		t.Errorf("interface constant is wrong:\n%s", rendered)
	}
}

// Every class the runtime declares has to render, since one that does not is a
// hole in what a fixture can be written against.
func TestEveryDefinitionRenders(t *testing.T) {
	definitions := jvm.CoreLibraryDefinitions()
	if len(definitions) == 0 {
		t.Fatal("the core library declares nothing")
	}
	for _, definition := range definitions {
		rendered := source(definition)
		_, simpleName := split(definition.Name)
		if !strings.Contains(rendered, simpleName) {
			t.Errorf("%s did not render its own name:\n%s", definition.Name, rendered)
		}
		// A JVM member name javac cannot read is the one thing rendering can
		// leak through: the constructors and the class initializer are named
		// with angle brackets and nothing else in a signature is.
		if strings.ContainsAny(rendered, "<>") {
			t.Errorf("%s rendered a JVM member name javac cannot read:\n%s", definition.Name, rendered)
		}
	}
}
