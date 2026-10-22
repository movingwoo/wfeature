package wipi

import "github.com/movingwoo/wfeature/internal/jvm"

// org.kwis.msp.db is WIPI's record database: named lists of records, which is
// where a Jlet keeps its progress when it does not write a file. Only the
// shapes are declared here. Every member that reads or writes records is
// native, because where the records live — and what a later session finds — is
// the save boundary's business, which the platform owns (see wipi_database.go
// in internal/platform/skt), and what each answers is the WIPI storage
// contract's (docs/wipi-storage.md).

// dataBaseExceptionDefinition declares one of the database's two exceptions.
// The specification has each extend Exception directly rather than one the
// other, so a catch of DataBaseException does not take a record error. A title
// is compiled against a handset's library, and its handlers were written for
// that hierarchy; one where a record error is also a DataBaseException would
// send it to a handler written for something else.
func dataBaseExceptionDefinition(name string) jvm.ClassDefinition {
	return jvm.ClassDefinition{
		Name:      name,
		SuperName: "java/lang/Exception",
		Access:    jvm.AccessPublic,
		Methods: []jvm.MethodDefinition{
			{Name: "<init>", Descriptor: "()V", Access: publicMethod, Body: superInit("java/lang/Exception", "()V")},
			{Name: "<init>", Descriptor: "(Ljava/lang/String;)V", Access: publicMethod, Body: superInit("java/lang/Exception", "(Ljava/lang/String;)V")},
		},
	}
}

// dataFilterDefinition and dataComparatorDefinition are the two callbacks
// sortRecord takes, both implemented by the title.
func dataFilterDefinition() jvm.ClassDefinition {
	return jvm.ClassDefinition{
		Name:      DataFilterClass,
		SuperName: "java/lang/Object",
		Access:    jvm.AccessPublic | jvm.AccessInterface | jvm.AccessAbstract,
		Methods: []jvm.MethodDefinition{
			{Name: "filter", Descriptor: "([B)Z", Access: jvm.AccessPublic | jvm.AccessAbstract},
		},
	}
}

// The comparator's three answers have no numbers in the specification's pages;
// these are the WIPI storage contract's (docs/wipi-storage.md), MIDP's
// RecordComparator values, which this interface copies name for name. A title
// compiles the constants into its own code, and the platform's sort reads only
// the sign of an answer (see wipiDataBaseSortRecord).
func dataComparatorDefinition() jvm.ClassDefinition {
	return jvm.ClassDefinition{
		Name:      DataComparatorClass,
		SuperName: "java/lang/Object",
		Access:    jvm.AccessPublic | jvm.AccessInterface | jvm.AccessAbstract,
		Fields: []jvm.FieldDefinition{
			{Name: "EQUIVALENT", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(0)},
			{Name: "FOLLOWS", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(1)},
			{Name: "PRECEDES", Descriptor: "I", Access: constantField, Constant: jvm.IntValue(-1)},
		},
		Methods: []jvm.MethodDefinition{
			{Name: "compare", Descriptor: "([B[B)I", Access: jvm.AccessPublic | jvm.AccessAbstract},
		},
	}
}

// dataBaseDefinition is the database itself. A title gets one from
// openDataBase — the class has no public constructor — and every member is
// the platform's.
func dataBaseDefinition() jvm.ClassDefinition {
	const (
		text     = "Ljava/lang/String;"
		database = "Lorg/kwis/msp/db/DataBase;"
	)
	stored := []string{DataBaseExceptionClass}
	record := []string{DataBaseExceptionClass, DataBaseRecordExceptionClass}
	ranged := []string{DataBaseExceptionClass, DataBaseRecordExceptionClass, "java/lang/IllegalArgumentException"}
	opened := []string{DataBaseExceptionClass, "java/lang/IllegalArgumentException"}
	return jvm.ClassDefinition{
		Name:      DataBaseClass,
		SuperName: "java/lang/Object",
		Access:    jvm.AccessPublic,
		Methods: []jvm.MethodDefinition{
			{Name: "openDataBase", Descriptor: "(" + text + "IZ)" + database, Access: publicStaticNat, Throws: opened},
			{Name: "openDataBase", Descriptor: "(" + text + "IZI)" + database, Access: publicStaticNat, Throws: opened},
			{Name: "closeDataBase", Descriptor: "()V", Access: publicNative, Throws: stored},
			{Name: "deleteDataBase", Descriptor: "(" + text + ")V", Access: publicStaticNat, Throws: stored},
			{Name: "deleteDataBase", Descriptor: "(" + text + "I)V", Access: publicStaticNat, Throws: stored},
			{Name: "listDataBases", Descriptor: "()[" + text, Access: publicStaticNat},
			{Name: "getAccessMode", Descriptor: "(" + text + ")I", Access: publicStaticNat, Throws: stored},
			{Name: "getDataBaseName", Descriptor: "()" + text, Access: publicNative},
			{Name: "getDataBaseSize", Descriptor: "()I", Access: publicNative},
			{Name: "getLastModified", Descriptor: "()J", Access: publicNative},
			{Name: "getNumberOfRecords", Descriptor: "()I", Access: publicNative},
			{Name: "getRecordSize", Descriptor: "()I", Access: publicNative},
			{Name: "getSizeAvailable", Descriptor: "()I", Access: publicNative},
			{Name: "insertRecord", Descriptor: "([B)I", Access: publicNative, Throws: record},
			{Name: "insertRecord", Descriptor: "([BII)I", Access: publicNative, Throws: ranged},
			{Name: "selectRecord", Descriptor: "(I)[B", Access: publicNative, Throws: record},
			{Name: "selectRecord", Descriptor: "(I[BI)V", Access: publicNative, Throws: ranged},
			{Name: "updateRecord", Descriptor: "(I[B)V", Access: publicNative, Throws: record},
			{Name: "updateRecord", Descriptor: "(I[BII)V", Access: publicNative, Throws: ranged},
			{Name: "deleteRecord", Descriptor: "(I)V", Access: publicNative, Throws: record},
			{Name: "sortRecord", Descriptor: "(Lorg/kwis/msp/db/DataFilter;Lorg/kwis/msp/db/DataComparator;)[I", Access: publicNative, Throws: stored},
		},
	}
}
