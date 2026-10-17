package lgt

import (
	"context"
	"encoding/binary"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// `org/kwis/msp/db/DataBase`, which is where a Java title of this platform puts
// its save when it does not write a file. Four local titles open one during
// startApp, so being without it is a title that does not start rather than a
// title that loses its progress.
//
// **It is stored as one file per database in the same store a Clet writes
// into.** That is the platform's own arrangement — the specification says the
// records live in the platform's persistent area and that deleting the Jlet
// deletes them — and putting it beside the files means one save directory per
// game rather than two stores that can disagree.
//
// The container is this runtime's own, because nothing here has to read a
// handset's: a header naming the record size and the record count, then one
// length-prefixed record each. A deleted record keeps its slot with the length
// below, so record identifiers stay stable and are reused the way the
// specification says they are.

const (
	javaDataBaseClass          = "org/kwis/msp/db/DataBase"
	javaDataBaseExceptionClass = "org/kwis/msp/db/DataBaseException"
	javaDataBaseRecordClass    = "org/kwis/msp/db/DataBaseRecordException"
	// databaseMagic and databaseVersion head every container, so a file that is
	// not one is refused rather than read as an empty database.
	databaseMagic   = "WFDB"
	databaseVersion = 1
	// databaseDeleted is the length that marks a slot whose record was
	// deleted. A real length can never reach it.
	databaseDeleted = ^uint32(0)
	// databaseSuffix is what a database's name becomes in the file store. It
	// keeps a database and a file of the same name apart.
	databaseSuffix = ".db"
	// databaseCapacity is what getSizeAvailable answers from. Nothing here runs
	// out of room; the number exists because a title divides by it.
	databaseCapacity = 1 << 20
	maxDatabaseName  = 128
)

// javaDatabase is one DataBase object: the database it stands for and whether
// the title has closed it. What the database holds is the databaseContent it
// shares with every other object open on the database.
type javaDatabase struct {
	name string
	// closed marks a database the title has closed. The entry is kept rather
	// than dropped, because closing one twice is defined and the second close
	// has to be able to tell a database it already wrote from an object that
	// was never a database at all. See javaCloseDataBase.
	closed bool
	*databaseContent
}

// databaseContent is a database's records as the objects open on it hold
// them.
//
// **Every object on one database is one set of records.** A handset's objects
// share one persistent store, so a record one object writes is what another
// reads next. A copy per object kept a write the store had refused in the one
// object that made it: another object read the container without it, stored
// its own change over it, and the first object's close then put its older
// copy back over that.
type databaseContent struct {
	recordSize uint32
	records    [][]byte
	// deleted marks the slots whose record was removed. The slot stays so the
	// identifiers above it do not move.
	deleted  []bool
	modified int64
	// unsaved says the last store of these records was refused: they are
	// ahead of the container. The next store the store takes clears it, and a
	// quick save, a quick load and the ending ask again.
	unsaved bool
	// synced is the container's epoch when these records were last level with
	// it: at the open, at each store the store took, and at each read of it
	// again. See fileContent.synced and javaDatabaseOf.
	synced uint64
}

// javaDatabaseMethods is this class's whole surface. It joins the platform
// table in java_api.go's init.
var javaDatabaseMethods = map[string]javaPlatformMethod{
	"org/kwis/msp/db/DataBase.openDataBase(Ljava/lang/String;IZ)Lorg/kwis/msp/db/DataBase;": {
		Words: 3, Implementat: javaOpenDataBase},
	// The four-argument form names a sharing flag. There is one application
	// and one private store here, so there is nothing for it to select.
	"org/kwis/msp/db/DataBase.openDataBase(Ljava/lang/String;IZI)Lorg/kwis/msp/db/DataBase;": {
		Words: 4, Implementat: javaOpenDataBase},
	"org/kwis/msp/db/DataBase.closeDataBase()V": {Words: 1, Implementat: javaCloseDataBase},
	"org/kwis/msp/db/DataBase.getDataBaseName()Ljava/lang/String;": {
		Words: 1, Implementat: javaDataBaseName},
	"org/kwis/msp/db/DataBase.getNumberOfRecords()I": {Words: 1, Implementat: javaDataBaseRecordCount},
	"org/kwis/msp/db/DataBase.getRecordSize()I":      {Words: 1, Implementat: javaDataBaseRecordSize},
	"org/kwis/msp/db/DataBase.getDataBaseSize()I":    {Words: 1, Implementat: javaDataBaseSize},
	"org/kwis/msp/db/DataBase.getSizeAvailable()I":   {Words: 1, Implementat: javaDataBaseAvailable},
	"org/kwis/msp/db/DataBase.getLastModified()J":    {Words: 1, Implementat: javaDataBaseModified},
	"org/kwis/msp/db/DataBase.insertRecord([B)I":     {Words: 2, Implementat: javaInsertRecord},
	"org/kwis/msp/db/DataBase.insertRecord([BII)I":   {Words: 4, Implementat: javaInsertRecord},
	"org/kwis/msp/db/DataBase.updateRecord(I[B)V":    {Words: 3, Implementat: javaUpdateRecord},
	"org/kwis/msp/db/DataBase.updateRecord(I[BII)V":  {Words: 5, Implementat: javaUpdateRecord},
	"org/kwis/msp/db/DataBase.selectRecord(I)[B":     {Words: 2, Implementat: javaSelectRecord},
	"org/kwis/msp/db/DataBase.selectRecord(I[BI)V":   {Words: 4, Implementat: javaSelectRecordInto},
	"org/kwis/msp/db/DataBase.deleteRecord(I)V":      {Words: 2, Implementat: javaDeleteRecord},
	"org/kwis/msp/db/DataBase.listDataBases()[Ljava/lang/String;": {
		Implementat: javaListDataBases},
	"org/kwis/msp/db/DataBase.deleteDataBase(Ljava/lang/String;)V": {
		Words: 1, Implementat: javaDeleteDataBase},
	"org/kwis/msp/db/DataBase.deleteDataBase(Ljava/lang/String;I)V": {
		Words: 2, Implementat: javaDeleteDataBase},
	// The access mode of a database this application owns. There is one
	// application, so what it can do to its own database is everything.
	"org/kwis/msp/db/DataBase.getAccessMode(Ljava/lang/String;)I": {
		Words: 1, Implementat: javaDataBaseAccessMode},
}

// javaOpenDataBase is `openDataBase(name, recordSize, create)`. It answers a
// DataBase object, and the specification says it throws when the database is
// not there and the caller did not ask for it to be created.
func javaOpenDataBase(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	name, ok := client.javaText(arguments[0])
	if !ok {
		return 0, fmt.Errorf("the name at %#x is not a string this platform built", arguments[0])
	}
	if err := validDatabaseName(name); err != nil {
		return 0, client.throwJavaPlatform(thread, javaDataBaseExceptionClass, ": "+err.Error())
	}
	recordSize, create := arguments[1], arguments[2] != 0
	stored, exists := client.readFile(databaseFileName(name))
	// An object the title already has on the database is the database. Its
	// records are the newest there are when they hold a write the store
	// refused, and then they are the database whatever the store says.
	shared := client.openDatabaseContent(name)
	if shared != nil && shared.unsaved {
		exists = true
	}
	if !exists && !create {
		return 0, client.throwJavaPlatform(thread, javaDataBaseExceptionClass,
			fmt.Sprintf(": %q does not exist", name))
	}
	content := shared
	if content == nil || !content.unsaved {
		fresh := &databaseContent{recordSize: recordSize, synced: client.fileEpoch(databaseFileName(name))}
		if exists {
			if err := fresh.decode(stored); err != nil {
				return 0, client.throwJavaPlatform(thread, javaDataBaseExceptionClass,
					fmt.Sprintf(": %q: %v", name, err))
			}
		}
		if content == nil {
			content = fresh
		} else {
			// The records as the container has them now, for every object on
			// the database at once.
			fresh.modified = content.modified
			*content = *fresh
		}
	}
	database := &javaDatabase{name: name, databaseContent: content}
	class, err := client.preparePlatformJavaClass(javaDataBaseClass)
	if err != nil {
		return 0, err
	}
	object, err := client.allocateJavaObject(class)
	if err != nil {
		return 0, err
	}
	client.javaRuntimeState().databases[object] = database
	if !exists {
		client.storeDatabase(database)
		client.recordDatabaseName(name, true)
	}
	if client.logger != nil {
		client.logger.Debug("LGT java database opened",
			"name", name, "records", len(database.records), "record_size", recordSize, "created", !exists)
	}
	return object, nil
}

func validDatabaseName(name string) error {
	if name == "" || len(name) > maxDatabaseName {
		return fmt.Errorf("%q is not a database name", name)
	}
	if strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("%q names a path rather than a database", name)
	}
	return nil
}

func databaseFileName(name string) string {
	return name + databaseSuffix
}

// openDatabaseContent answers the records the objects on a database share
// while one of them is open, or holds a write the store refused; nil when no
// object does.
func (client *Client) openDatabaseContent(name string) *databaseContent {
	for _, object := range slices.Sorted(maps.Keys(client.javaRuntimeState().databases)) {
		database := client.javaRun.databases[object]
		if database != nil && database.name == name && database.databaseContent != nil && (!database.closed || database.unsaved) {
			return database.databaseContent
		}
	}
	return nil
}

// databaseContents answers one object for each set of records, the lowest
// numbered one, in object order, and whether any object on those records is
// still open: what the ending and a quick step store, once each.
func (client *Client) databaseContents() ([]*javaDatabase, map[*databaseContent]bool) {
	if client.javaRun == nil {
		return nil, nil
	}
	open := make(map[*databaseContent]bool)
	databases := make([]*javaDatabase, 0, len(client.javaRun.databases))
	for _, object := range slices.Sorted(maps.Keys(client.javaRun.databases)) {
		database := client.javaRun.databases[object]
		if database == nil || database.databaseContent == nil {
			continue
		}
		if _, seen := open[database.databaseContent]; !seen {
			databases = append(databases, database)
			open[database.databaseContent] = false
		}
		if !database.closed {
			open[database.databaseContent] = true
		}
	}
	return databases, open
}

// javaDatabaseOf answers the database behind an object, and refuses one that is
// closed or was never opened here.
//
// **The records are read again when the container moved on.** The objects on
// a database share one set of records (see databaseContent), and every change
// stores it whole; something other than those objects can still change the
// container — the title deleting the database, a file written under its name.
// So records whose container has been stored or deleted since they were last
// level with it are read again before an object answers, and a write lands on
// the records as they are. Records holding a write the store refused are kept,
// because they are the only place that write is.
func (client *Client) javaDatabaseOf(object uint32) (*javaDatabase, error) {
	database, ok := client.javaRuntimeState().databases[object]
	if !ok {
		return nil, fmt.Errorf("the object at %#x is not an open database", object)
	}
	if database.closed {
		return nil, fmt.Errorf("the database %q is closed", database.name)
	}
	if !database.unsaved && database.synced != client.fileEpoch(databaseFileName(database.name)) {
		client.reloadDatabase(database)
	}
	return database, nil
}

// reloadDatabase reads an object's container again. A deleted container is an
// empty database, as it is to an object a quick load fills. A container that
// cannot be read or decoded leaves the object as it was.
func (client *Client) reloadDatabase(database *javaDatabase) {
	name := databaseFileName(database.name)
	epoch := client.fileEpoch(name)
	stored, exists := client.readFile(name)
	if client.saveReadError != nil {
		return
	}
	fresh := &databaseContent{recordSize: database.recordSize}
	if exists {
		if err := fresh.decode(stored); err != nil {
			if client.logger != nil {
				client.logger.Debug("LGT java database could not be read again", "name", database.name, "error", err)
			}
			return
		}
	}
	database.records, database.deleted, database.recordSize, database.synced = fresh.records, fresh.deleted, fresh.recordSize, epoch
}

// storeDatabase writes a database back. Every change goes through it, because a
// title that is killed between a write and a close should still have what it
// stored — which is what the specification promises of a record already saved.
func (client *Client) storeDatabase(database *javaDatabase) {
	database.modified = client.clock.unixMillis()
	database.unsaved = client.storeFile(databaseFileName(database.name), database.encode()) != nil
	if !database.unsaved {
		database.synced = client.fileEpoch(databaseFileName(database.name))
	}
}

// encode lays the container out: the magic and version, the record size, the
// count, then one length-prefixed record each.
func (database *databaseContent) encode() []byte {
	data := make([]byte, 0, 16)
	data = append(data, databaseMagic...)
	var header [12]byte
	binary.LittleEndian.PutUint32(header[0:], databaseVersion)
	binary.LittleEndian.PutUint32(header[4:], database.recordSize)
	binary.LittleEndian.PutUint32(header[8:], uint32(len(database.records)))
	data = append(data, header[:]...)
	for index, record := range database.records {
		var length [4]byte
		if database.deleted[index] {
			binary.LittleEndian.PutUint32(length[:], databaseDeleted)
			data = append(data, length[:]...)
			continue
		}
		binary.LittleEndian.PutUint32(length[:], uint32(len(record)))
		data = append(data, length[:]...)
		data = append(data, record...)
	}
	return data
}

func (database *databaseContent) decode(data []byte) error {
	if len(data) < 16 || string(data[:4]) != databaseMagic {
		return fmt.Errorf("is not a database container")
	}
	if version := binary.LittleEndian.Uint32(data[4:]); version != databaseVersion {
		return fmt.Errorf("is version %d, and this runtime writes %d", version, databaseVersion)
	}
	// The record size the container was created with wins over the one the
	// caller passed: a title that reopens its save with a different size is
	// reading records the first size wrote.
	database.recordSize = binary.LittleEndian.Uint32(data[8:])
	count := binary.LittleEndian.Uint32(data[12:])
	if count > maxJavaArrayLength {
		return fmt.Errorf("claims %d records", count)
	}
	cursor := 16
	for index := uint32(0); index < count; index++ {
		if cursor+4 > len(data) {
			return fmt.Errorf("ends inside record %d", index)
		}
		length := binary.LittleEndian.Uint32(data[cursor:])
		cursor += 4
		if length == databaseDeleted {
			database.records = append(database.records, nil)
			database.deleted = append(database.deleted, true)
			continue
		}
		if uint64(cursor)+uint64(length) > uint64(len(data)) {
			return fmt.Errorf("record %d claims %d bytes", index, length)
		}
		record := make([]byte, length)
		copy(record, data[cursor:cursor+int(length)])
		cursor += int(length)
		database.records = append(database.records, record)
		database.deleted = append(database.deleted, false)
	}
	return nil
}

// javaCloseDataBase is `closeDataBase()`: the object stops being usable.
//
// **A close stores only a write the store refused.** Every change was stored
// when it was made, so what the object holds otherwise is what the container
// holds, or older: another object may have saved since, or the title deleted
// the database. Writing the object's copy back would undo the one and bring
// the other into being again.
//
// **Closing one that is already closed is ignored**, which the specification
// says in as many words, and one local title depends on it: it closes its
// database at the end of a save and again on the way out of the routine that
// called the save. The object stays in the table marked closed rather than
// being dropped, so the second close is told apart from a call on something
// that was never opened here — which is still a stop, because it means the
// title is holding an object this platform did not issue.
func javaCloseDataBase(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	database, ok := client.javaRuntimeState().databases[arguments[0]]
	if !ok {
		return 0, fmt.Errorf("the object at %#x is not a database this platform opened", arguments[0])
	}
	if database.closed {
		return 0, nil
	}
	if database.unsaved {
		client.storeDatabase(database)
	}
	database.closed = true
	return 0, nil
}

func javaDataBaseName(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	database, err := client.javaDatabaseOf(arguments[0])
	if err != nil {
		return 0, err
	}
	return client.newJavaString(database.name)
}

func javaDataBaseRecordCount(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	database, err := client.javaDatabaseOf(arguments[0])
	if err != nil {
		return 0, err
	}
	count := uint32(0)
	for index := range database.records {
		if !database.deleted[index] {
			count++
		}
	}
	return count, nil
}

func javaDataBaseRecordSize(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	database, err := client.javaDatabaseOf(arguments[0])
	if err != nil {
		return 0, err
	}
	return database.recordSize, nil
}

func javaDataBaseSize(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	database, err := client.javaDatabaseOf(arguments[0])
	if err != nil {
		return 0, err
	}
	return uint32(len(database.encode())), nil
}

// javaDataBaseAvailable answers what is left of a size this platform does not
// enforce. A title divides by it or compares it against a record it is about to
// write, so answering zero would stop a save that has nothing wrong with it.
func javaDataBaseAvailable(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	database, err := client.javaDatabaseOf(arguments[0])
	if err != nil {
		return 0, err
	}
	used := uint32(len(database.encode()))
	if used >= databaseCapacity {
		return 0, nil
	}
	return databaseCapacity - used, nil
}

func javaDataBaseModified(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	database, err := client.javaDatabaseOf(arguments[0])
	if err != nil {
		return 0, err
	}
	milliseconds := uint64(database.modified)
	if err := thread.SetRegister(1, uint32(milliseconds>>32)); err != nil {
		return 0, err
	}
	return uint32(milliseconds), nil
}

// javaRecordBytes reads the array a record call was handed, honouring the
// offset and length the four-argument forms carry.
func (client *Client) javaRecordBytes(array, offset, count uint32, windowed bool) ([]byte, error) {
	data, err := client.readJavaArrayBytes(array)
	if err != nil {
		return nil, err
	}
	if !windowed {
		return data, nil
	}
	if uint64(offset)+uint64(count) > uint64(len(data)) {
		return nil, fmt.Errorf("%d bytes from %d is past the end of %d", count, offset, len(data))
	}
	return data[offset : offset+count], nil
}

func javaInsertRecord(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	database, err := client.javaDatabaseOf(arguments[0])
	if err != nil {
		return 0, err
	}
	record, err := client.javaRecordBytes(arguments[1], argumentAt(arguments, 2), argumentAt(arguments, 3), len(arguments) > 3)
	if err != nil {
		return 0, err
	}
	if err := database.fits(record); err != nil {
		return 0, client.throwJavaPlatform(thread, javaDataBaseRecordClass, ": "+err.Error())
	}
	stored := make([]byte, len(record))
	copy(stored, record)
	// A deleted slot is reused before the end is grown, which is what the
	// specification says a record identifier does.
	identifier := len(database.records)
	for index, gone := range database.deleted {
		if gone {
			identifier = index
			break
		}
	}
	if identifier == len(database.records) {
		database.records = append(database.records, stored)
		database.deleted = append(database.deleted, false)
	} else {
		database.records[identifier] = stored
		database.deleted[identifier] = false
	}
	client.storeDatabase(database)
	return uint32(identifier), nil
}

// fits refuses a record longer than the size the database was created with,
// which is the one limit the specification puts on a record.
func (database *databaseContent) fits(record []byte) error {
	if database.recordSize == 0 || uint64(len(record)) <= uint64(database.recordSize) {
		return nil
	}
	return fmt.Errorf("a %d-byte record does not fit a %d-byte record size", len(record), database.recordSize)
}

func argumentAt(arguments []uint32, index int) uint32 {
	if index < len(arguments) {
		return arguments[index]
	}
	return 0
}

func javaUpdateRecord(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	database, err := client.javaDatabaseOf(arguments[0])
	if err != nil {
		return 0, err
	}
	record, err := client.javaRecordBytes(arguments[2], argumentAt(arguments, 3), argumentAt(arguments, 4), len(arguments) > 4)
	if err != nil {
		return 0, err
	}
	identifier := arguments[1]
	if err := database.holds(identifier); err != nil {
		return 0, client.throwJavaPlatform(thread, javaDataBaseRecordClass, ": "+err.Error())
	}
	if err := database.fits(record); err != nil {
		return 0, client.throwJavaPlatform(thread, javaDataBaseRecordClass, ": "+err.Error())
	}
	stored := make([]byte, len(record))
	copy(stored, record)
	database.records[identifier] = stored
	client.storeDatabase(database)
	return 0, nil
}

func (database *javaDatabase) holds(identifier uint32) error {
	if uint64(identifier) >= uint64(len(database.records)) || database.deleted[identifier] {
		return fmt.Errorf("record %d is not in %q", identifier, database.name)
	}
	return nil
}

func javaSelectRecord(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	database, err := client.javaDatabaseOf(arguments[0])
	if err != nil {
		return 0, err
	}
	if err := database.holds(arguments[1]); err != nil {
		return 0, client.throwJavaPlatform(thread, javaDataBaseRecordClass, ": "+err.Error())
	}
	return client.newJavaByteArray(database.records[arguments[1]])
}

// javaSelectRecordInto is the form that fills the caller's array.
func javaSelectRecordInto(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	database, err := client.javaDatabaseOf(arguments[0])
	if err != nil {
		return 0, err
	}
	if err := database.holds(arguments[1]); err != nil {
		return 0, client.throwJavaPlatform(thread, javaDataBaseRecordClass, ": "+err.Error())
	}
	record := database.records[arguments[1]]
	block, err := client.readWord(arguments[2] + 8)
	if err != nil {
		return 0, err
	}
	length, err := client.readWord(block)
	if err != nil {
		return 0, err
	}
	offset := arguments[3]
	if uint64(offset)+uint64(len(record)) > uint64(length) {
		return 0, client.throwJavaPlatform(thread, javaDataBaseRecordClass,
			fmt.Sprintf(": a %d-byte record at %d does not fit a %d-byte buffer", len(record), offset, length))
	}
	return 0, client.core.Memory().Write(block+javaArrayLengthWords*4+offset, record)
}

func javaDeleteRecord(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	database, err := client.javaDatabaseOf(arguments[0])
	if err != nil {
		return 0, err
	}
	if err := database.holds(arguments[1]); err != nil {
		return 0, client.throwJavaPlatform(thread, javaDataBaseRecordClass, ": "+err.Error())
	}
	database.records[arguments[1]] = nil
	database.deleted[arguments[1]] = true
	client.storeDatabase(database)
	return 0, nil
}

// javaListDataBases answers the names this application has created.
//
// **The store the containers live in cannot be listed**: it is a key-value
// store a browser Host also implements, and asking it for its keys is not part
// of that contract. So the names are kept in one more entry beside them, which
// every create and every delete rewrites.
func javaListDataBases(
	client *Client, _ context.Context, _ *armcore.Thread, _ []uint32,
) (uint32, error) {
	return client.newJavaStringArray(client.databaseNames())
}

// databaseIndexName is the entry the names are kept in. It carries the suffix
// so that a title with a database of the same name cannot collide with it.
const databaseIndexName = "__databases__" + databaseSuffix

func (client *Client) databaseNames() []string {
	data, ok := client.readFile(databaseIndexName)
	if !ok {
		return nil
	}
	names := make([]string, 0, 8)
	for _, name := range strings.Split(string(data), "\n") {
		if name = strings.TrimSpace(name); name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func (client *Client) recordDatabaseName(name string, present bool) {
	names := client.databaseNames()
	kept := make([]string, 0, len(names)+1)
	for _, existing := range names {
		if existing != name {
			kept = append(kept, existing)
		}
	}
	if present {
		kept = append(kept, name)
	}
	sort.Strings(kept)
	client.writeFile(databaseIndexName, []byte(strings.Join(kept, "\n")))
}

func javaDeleteDataBase(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	name, ok := client.javaText(arguments[0])
	if !ok {
		return 0, fmt.Errorf("the name at %#x is not a string this platform built", arguments[0])
	}
	_, exists := client.readFile(databaseFileName(name))
	shared := client.openDatabaseContent(name)
	if !exists && (shared == nil || !shared.unsaved) {
		return 0, client.throwJavaPlatform(thread, javaDataBaseExceptionClass,
			fmt.Sprintf(": %q does not exist", name))
	}
	client.removeFile(databaseFileName(name))
	// A write the store refused goes with the database, as a write waiting on
	// a removed file goes with the file: neither a close nor the ending may
	// bring the database back with it. The objects still open on it read it
	// empty, as they would once they read the deleted container again.
	if shared != nil {
		shared.records, shared.deleted = nil, nil
		shared.unsaved, shared.synced = false, client.fileEpoch(databaseFileName(name))
	}
	client.recordDatabaseName(name, false)
	return 0, nil
}

// javaDataBaseAccessMode answers what this application may do to a database it
// owns, which is everything. The specification's modes are read, write and the
// two together; there is one application here and one store.
func javaDataBaseAccessMode(
	_ *Client, _ context.Context, _ *armcore.Thread, _ []uint32,
) (uint32, error) {
	return javaDataBaseReadWrite, nil
}

// javaDataBaseReadWrite is the specification's READ_WRITE access mode.
const javaDataBaseReadWrite = 3
