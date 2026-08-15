package jvm

import "testing"

func TestParseMethodDescriptor(t *testing.T) {
	descriptor, err := ParseMethodDescriptor("(IJLjava/lang/String;[[B)D")
	if err != nil {
		t.Fatalf("ParseMethodDescriptor() error = %v", err)
	}
	if len(descriptor.Parameters) != 4 {
		t.Fatalf("len(Parameters) = %d", len(descriptor.Parameters))
	}
	if descriptor.Parameters[0].Kind != TypeInt || descriptor.Parameters[1].Kind != TypeLong {
		t.Fatalf("numeric parameters = %+v", descriptor.Parameters[:2])
	}
	if descriptor.Parameters[2].ClassName != "java/lang/String" {
		t.Fatalf("reference parameter = %+v", descriptor.Parameters[2])
	}
	if descriptor.Parameters[3].Kind != TypeArray || descriptor.Parameters[3].Component.Kind != TypeArray {
		t.Fatalf("array parameter = %+v", descriptor.Parameters[3])
	}
	if descriptor.Return.Kind != TypeDouble {
		t.Fatalf("return = %+v", descriptor.Return)
	}
}

func TestParseFieldDescriptor(t *testing.T) {
	typeInfo, err := ParseFieldDescriptor("[Ljava/lang/Object;")
	if err != nil {
		t.Fatalf("ParseFieldDescriptor() error = %v", err)
	}
	if typeInfo.Kind != TypeArray || typeInfo.Component.ClassName != "java/lang/Object" {
		t.Fatalf("type = %+v", typeInfo)
	}
}

func TestRejectInvalidDescriptors(t *testing.T) {
	tests := []string{"", "I", "(V)V", "(I", "(I)Vx", "([V)V", "(L;)V"}
	for _, descriptor := range tests {
		t.Run(descriptor, func(t *testing.T) {
			if _, err := ParseMethodDescriptor(descriptor); err == nil {
				t.Fatalf("ParseMethodDescriptor(%q) succeeded", descriptor)
			}
		})
	}
	if _, err := ParseFieldDescriptor("V"); err == nil {
		t.Fatal("ParseFieldDescriptor accepted void")
	}
}
