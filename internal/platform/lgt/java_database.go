package lgt

import (
	"bytes"
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
// **It is stored as one container per database in the store the files are
// in, in a scope of its own** (java_database_store.go). The specification says
// the records live in the platform's persistent area and that deleting the
// Jlet deletes them, so they are in the game's one save directory; and they
// are apart from its files, as on the other platforms, so a file and a
// database of the same name are two things.
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
	// The two interfaces sortRecord calls back into, both implemented by the
	// title: one method each.
	javaDataFilterInterface     = "org/kwis/msp/db/DataFilter"
	javaDataComparatorInterface = "org/kwis/msp/db/DataComparator"
	javaIllegalArgumentClass    = "java/lang/IllegalArgumentException"
	// databaseMagic and databaseVersion head every container, so a file that is
	// not one is refused rather than read as an empty database.
	databaseMagic   = "WFDB"
	databaseVersion = 1
	// databaseDeleted is the length that marks a slot whose record was
	// deleted. A real length can never reach it.
	databaseDeleted = ^uint32(0)
	// databaseSuffix ends a container's key: in the database scope
	// (databaseSaveKey), and among the files where earlier builds kept it
	// (databaseFileName).
	databaseSuffix = ".db"
	// maxDatabaseBytes bounds one database's records together: the common
	// contract's bound on untrusted input (docs/wipi-storage.md). A write that
	// would pass it is a DataBaseException; no title's save comes near it.
	maxDatabaseBytes = 4 << 20
	// javaPrivateAccess is FileSystem.PRIVATE_ACCESS, the one area a title has
	// and what getAccessMode answers for a database that exists.
	javaPrivateAccess = 1
)

// javaDatabase is one DataBase object: the database it stands for. What the
// database holds is the databaseContent it shares with every other object on
// the database.
type javaDatabase struct {
	name string
	// closed marks the records of an object the collector reclaimed, which
	// wait only for the store (see keepCollectedDatabases). A title's own
	// close releases nothing it can see and leaves this unset: the object
	// stays usable. See javaCloseDataBase.
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
	// unlisted says the title deleted the database while objects on it were
	// still held: a write through one of them makes the database again, and
	// puts its name back on the index. See javaDeleteDataBase.
	unlisted bool
}

// recordBytes is what the records hold together: getDataBaseSize's answer,
// what the space count takes for the database, and what maxDatabaseBytes
// bounds.
func (database *databaseContent) recordBytes() int {
	total := 0
	for index, record := range database.records {
		if !database.deleted[index] {
			total += len(record)
		}
	}
	return total
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
	"org/kwis/msp/db/DataBase.sortRecord(Lorg/kwis/msp/db/DataFilter;Lorg/kwis/msp/db/DataComparator;)[I": {
		Words: 3, Implementat: javaSortRecord},
	"org/kwis/msp/db/DataBase.listDataBases()[Ljava/lang/String;": {
		Implementat: javaListDataBases},
	"org/kwis/msp/db/DataBase.deleteDataBase(Ljava/lang/String;)V": {
		Words: 1, Implementat: javaDeleteDataBase},
	"org/kwis/msp/db/DataBase.deleteDataBase(Ljava/lang/String;I)V": {
		Words: 2, Implementat: javaDeleteDataBase},
	// The area a database is in, which is the title's own for every database
	// here. See javaDataBaseAccessMode.
	"org/kwis/msp/db/DataBase.getAccessMode(Ljava/lang/String;)I": {
		Words: 1, Implementat: javaDataBaseAccessMode},
}

// javaOpenDataBase is `openDataBase(name, recordSize, create)`. It answers a
// DataBase object. As the specification and the common contract
// (docs/wipi-storage.md) give it: a name no database can have is
// DataBaseException; create with a record size of 0 or less is
// IllegalArgumentException; a database that is not there is DataBaseException
// without create, and is made at once with it, empty and listed.
func javaOpenDataBase(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	name, err := client.javaDatabaseName(thread, arguments[0])
	if err != nil {
		return 0, err
	}
	recordSize, create := int32(arguments[1]), arguments[2] != 0
	if create && recordSize <= 0 {
		return 0, client.throwJavaPlatform(thread, javaIllegalArgumentClass,
			fmt.Sprintf(": a record size of %d", recordSize))
	}
	stored, exists := client.readDatabase(name)
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
		fresh := &databaseContent{recordSize: uint32(recordSize), synced: client.databaseEpoch(name)}
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
			fresh.modified, fresh.unlisted = content.modified, content.unlisted
			*content = *fresh
		}
	}
	// A container an earlier build made without a record size takes the size
	// of the open that finds it, and keeps it: it is stored with it at once.
	adopted := exists && content.recordSize == 0 && recordSize > 0
	if adopted {
		content.recordSize = uint32(recordSize)
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
	client.noteDatabaseTime(database, false)
	switch {
	case !exists:
		client.saveDatabase(database)
		client.recordDatabaseName(name, true)
	case adopted:
		client.saveDatabase(database)
	}
	if client.logger != nil {
		client.logger.Debug("LGT java database opened",
			"name", name, "records", len(database.records), "record_size", content.recordSize, "created", !exists)
	}
	return object, nil
}

// javaDatabaseName reads the name a database call is handed, and answers the
// database's name from then on (databasePath). Null, and a name no database
// can have, are the DataBaseException every DataBase call declares, as the
// common contract gives them (docs/wipi-storage.md).
func (client *Client) javaDatabaseName(thread *armcore.Thread, object uint32) (string, error) {
	if object == 0 {
		return "", client.throwJavaPlatform(thread, javaDataBaseExceptionClass, ": a null database name")
	}
	name, ok := client.javaText(object)
	if !ok {
		return "", fmt.Errorf("the name at %#x is not a string this platform built", object)
	}
	path, err := databasePath(name)
	if err != nil {
		return "", client.throwJavaPlatform(thread, javaDataBaseExceptionClass, ": "+err.Error())
	}
	return path, nil
}

// databasePath is a database's name as the store keeps it. **A database name
// is read as a file's name is** (guestPath): a backslash separates segments
// as a slash does, and leading separators, empty and "." segments are
// dropped, so every spelling of one path is one database, and a "/" inside
// the name puts the database in a directory, as it would put a file. The
// path is the name getDataBaseName and listDataBases answer.
//
// It refuses what no database can be kept under: the title's own directory,
// a path longer than the longest file name, one no file could have (a ".."
// segment, a name the platform keeps for itself), one whose container would
// be an index of names — among the files, where earlier builds kept it
// (databaseFileName), or below the one in the database scope
// (databaseIndexSegment) — and a line break, which an index, one name to a
// line, cannot hold. The container is asked about as well as the path because
// the suffix joins the last segment: ".." alone would otherwise be the
// container "...db".
func databasePath(name string) (string, error) {
	path := guestPath(name)
	first, _, nested := strings.Cut(path, "/")
	switch {
	case path == "":
		return "", fmt.Errorf("%q names the title's own directory, not a database", name)
	case len(path) > maxFileNameBytes:
		return "", fmt.Errorf("a name of %d bytes is longer than %d", len(path), maxFileNameBytes)
	case strings.ContainsAny(path, "\n\r"):
		return "", fmt.Errorf("%q is not one line of the index of names", name)
	case !hasSaveKey(path) || !hasSaveKey(databaseFileName(path)) || nested && first == databaseIndexSegment:
		return "", fmt.Errorf("%q is not a name the store can keep", name)
	}
	return path, nil
}

func databaseFileName(name string) string {
	return name + databaseSuffix
}

// openDatabaseContent answers the records the objects on a database share
// while one of them is open, or holds a write the store refused; nil when no
// object does. The records of an object the collector reclaimed with such a
// write are one of these.
func (client *Client) openDatabaseContent(name string) *databaseContent {
	runtime := client.javaRuntimeState()
	for _, object := range slices.Sorted(maps.Keys(runtime.databases)) {
		database := runtime.databases[object]
		if database != nil && database.name == name && database.databaseContent != nil && (!database.closed || database.unsaved) {
			return database.databaseContent
		}
	}
	for _, database := range runtime.collectedDatabases {
		if database.name == name && database.unsaved {
			return database.databaseContent
		}
	}
	return nil
}

// databaseContents answers one object for each set of records, the lowest
// numbered one, in object order, then the records of objects the collector
// reclaimed, and whether any object on those records is still open: what the
// ending and a quick step store, once each.
func (client *Client) databaseContents() ([]*javaDatabase, map[*databaseContent]bool) {
	if client.javaRun == nil {
		return nil, nil
	}
	open := make(map[*databaseContent]bool)
	databases := make([]*javaDatabase, 0, len(client.javaRun.databases)+len(client.javaRun.collectedDatabases))
	add := func(database *javaDatabase) {
		if database == nil || database.databaseContent == nil {
			return
		}
		if _, seen := open[database.databaseContent]; !seen {
			databases = append(databases, database)
			open[database.databaseContent] = false
		}
		if !database.closed {
			open[database.databaseContent] = true
		}
	}
	for _, object := range slices.Sorted(maps.Keys(client.javaRun.databases)) {
		add(client.javaRun.databases[object])
	}
	for _, database := range client.javaRun.collectedDatabases {
		add(database)
	}
	return databases, open
}

// keepCollectedDatabases is what the collector does with the DataBase objects
// it reclaims. **A write the store refused outlives its object.** Every change
// is stored when it is made, so an object holds nothing the store lacks unless
// that store was refused; those records are then the only place the write is,
// and the ending and a quick step reach a database only through its objects.
// Dropped with the object, the write was lost although the title had made it
// and the session had not ended.
//
// So the reclaimed object is closed as the title would close it: records
// another object still has wait with that object, and the rest are given to
// the store now, as closeDataBase gives them. What the store refuses again
// waits here, closed, for the ending, a quick step, or the next open of the
// database, which takes them up as its own.
func (client *Client) keepCollectedDatabases(reclaimed []*javaDatabase) {
	client.dropStoredCollectedDatabases()
	runtime := client.javaRun
	held := make(map[*databaseContent]bool, len(runtime.databases)+len(runtime.collectedDatabases))
	for _, database := range runtime.databases {
		if database != nil {
			held[database.databaseContent] = true
		}
	}
	for _, database := range runtime.collectedDatabases {
		held[database.databaseContent] = true
	}
	for _, database := range reclaimed {
		if !database.unsaved || held[database.databaseContent] {
			continue
		}
		held[database.databaseContent] = true
		database.closed = true
		if _, err := client.storeRefusedDatabase(database, false); err != nil {
			if client.logger != nil {
				client.logger.Debug("LGT java database kept a write the store refused when its object was collected",
					"name", database.name, "error", err)
			}
			runtime.collectedDatabases = append(runtime.collectedDatabases, database)
		}
	}
}

// dropStoredCollectedDatabases lets go of the reclaimed records that no longer
// wait: the store took them, or they were let go as stale, or the title
// deleted the database.
func (client *Client) dropStoredCollectedDatabases() {
	if client.javaRun == nil {
		return
	}
	client.javaRun.collectedDatabases = slices.DeleteFunc(client.javaRun.collectedDatabases,
		func(database *javaDatabase) bool { return !database.unsaved })
}

// javaDatabaseOf answers the database behind an object, and refuses one that
// was never opened here. A closed one is still the database: closing releases
// nothing a title can see.
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
		return nil, fmt.Errorf("the object at %#x is not a database this platform opened", object)
	}
	if !database.unsaved && database.synced != client.databaseEpoch(database.name) {
		client.reloadDatabase(database)
	}
	return database, nil
}

// reloadDatabase reads an object's container again. A deleted container is an
// empty database, as it is to an object a quick load fills. A container that
// cannot be read or decoded leaves the object as it was.
func (client *Client) reloadDatabase(database *javaDatabase) {
	epoch := client.databaseEpoch(database.name)
	stored, exists := client.readDatabase(database.name)
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

// storeDatabase writes a database back after a change the title made. Every
// change goes through it, because a title that is killed between a write and a
// close should still have what it stored — which is what the specification
// promises of a record already saved.
func (client *Client) storeDatabase(database *javaDatabase) {
	client.noteDatabaseTime(database, true)
	client.saveDatabase(database)
}

// saveDatabase gives the store a database's records, with nothing the title
// did to account for: storeDatabase's half that a close, the create and a
// retry share. A database the title deleted while this object was held is
// made again by it, name and all.
func (client *Client) saveDatabase(database *javaDatabase) {
	database.unsaved = client.writeDatabase(database.name, database.encode()) != nil
	if !database.unsaved {
		database.synced = client.databaseEpoch(database.name)
	}
	if database.unlisted {
		database.unlisted = false
		client.recordDatabaseName(database.name, true)
	}
}

// noteDatabaseTime keeps what getLastModified answers: the guest time of the
// last write to a database in this session, or of the session's first open of
// it when nothing has been written since. A write moves it; an open sets it
// only when the session has none for the name yet.
func (client *Client) noteDatabaseTime(database *javaDatabase, wrote bool) {
	runtime := client.javaRuntimeState()
	if runtime.databaseModified == nil {
		runtime.databaseModified = make(map[string]int64)
	}
	when, seen := runtime.databaseModified[database.name]
	if wrote || !seen {
		when = client.clock.unixMillis()
		runtime.databaseModified[database.name] = when
	}
	database.modified = when
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

// javaCloseDataBase is `closeDataBase()`, which releases nothing a title can
// see: the object stays usable, and closing it again is nothing, as the
// common contract gives it. One local title closes its database at the end of
// a save and again on the way out of the routine that called the save.
//
// **A close stores only a write the store refused.** Every change was stored
// when it was made, so what the object holds otherwise is what the container
// holds, or older: another object may have saved since, or the title deleted
// the database. Writing the object's copy back would undo the one and bring
// the other into being again. A call on something that was never opened here
// is still a stop, because it means the title is holding an object this
// platform did not issue.
func javaCloseDataBase(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	database, ok := client.javaRuntimeState().databases[arguments[0]]
	if !ok {
		return 0, fmt.Errorf("the object at %#x is not a database this platform opened", arguments[0])
	}
	if database.unsaved {
		client.saveDatabase(database)
	}
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

// javaDataBaseRecordCount is `getNumberOfRecords`: the records held.
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

// javaDataBaseSize is `getDataBaseSize`: the bytes the records hold, not what
// their container takes in the store.
func javaDataBaseSize(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	database, err := client.javaDatabaseOf(arguments[0])
	if err != nil {
		return 0, err
	}
	return uint32(database.recordBytes()), nil
}

// javaDataBaseAvailable is `getSizeAvailable`: the room the title has left
// across everything it has stored, which is the figure MC_fsAvailable and
// FileSystem.available answer too (storageAvailable). Nothing is refused for
// room, so a title that compares it with a record it is about to write is
// told the truth and still saves.
func javaDataBaseAvailable(
	client *Client, _ context.Context, _ *armcore.Thread, arguments []uint32,
) (uint32, error) {
	if _, err := client.javaDatabaseOf(arguments[0]); err != nil {
		return 0, err
	}
	return uint32(client.storageAvailable()), nil
}

// javaDataBaseModified is `getLastModified`: the guest time of the last
// write to the database in this session, or of the session's first open of it
// when nothing has been written since (noteDatabaseTime).
func javaDataBaseModified(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	database, err := client.javaDatabaseOf(arguments[0])
	if err != nil {
		return 0, err
	}
	when := database.modified
	if recorded, ok := client.javaRuntimeState().databaseModified[database.name]; ok {
		when = recorded
	}
	milliseconds := uint64(when)
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
	// The record size is not enforced: a longer record is stored whole. The
	// specification refuses it, and a title that has always written such
	// records here would lose its save on the next write. What is refused is
	// a database past the bound on untrusted input.
	if err := database.room(0, len(record)); err != nil {
		return 0, client.throwJavaPlatform(thread, javaDataBaseExceptionClass, ": "+err.Error())
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

// room refuses a write that would take the records past maxDatabaseBytes:
// one that replaces `replaced` bytes with `adding`.
func (database *databaseContent) room(replaced, adding int) error {
	if total := database.recordBytes() - replaced + adding; total > maxDatabaseBytes {
		return fmt.Errorf("%d bytes of records is more than the %d one database holds", total, maxDatabaseBytes)
	}
	return nil
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
	if err := database.room(len(database.records[identifier]), len(record)); err != nil {
		return 0, client.throwJavaPlatform(thread, javaDataBaseExceptionClass, ": "+err.Error())
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

// javaSelectRecordInto is the form that fills the caller's array: the bytes
// stored, copied at the offset. A buffer they do not fit, from that offset,
// is an IllegalArgumentException.
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
	if arguments[2] == 0 {
		return 0, client.throwJavaPlatform(thread, javaThrowNullClass, ": selectRecord into a null buffer")
	}
	record := database.records[arguments[1]]
	block, length, err := client.javaArrayBlock(arguments[2])
	if err != nil {
		return 0, err
	}
	offset := int32(arguments[3])
	if offset < 0 || int64(offset)+int64(len(record)) > int64(length) {
		return 0, client.throwJavaPlatform(thread, javaIllegalArgumentClass,
			fmt.Sprintf(": a %d-byte record at %d does not fit a %d-byte buffer", len(record), offset, length))
	}
	return 0, client.core.Memory().Write(block+javaArrayLengthWords*4+uint32(offset), record)
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

// javaListDataBases answers the names of the databases the title has, saved
// in any session or opened in this one, in name order, and null when there is
// none.
//
// **The store the containers live in cannot be listed**: it is a key-value
// store a browser Host also implements, and asking it for its keys is not part
// of that contract. So the names are kept in one more entry beside them, which
// every create and every delete rewrites. A name whose container a file call
// removed is not a database the title has any more, and is left out, and so
// is a name an earlier build wrote that is no database name now ("." or
// ".."), which no open could reach.
func javaListDataBases(
	client *Client, _ context.Context, _ *armcore.Thread, _ []uint32,
) (uint32, error) {
	var names []string
	for _, name := range client.databaseNames() {
		if path, err := databasePath(name); err == nil && path == name && client.databaseExists(name) {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return 0, nil
	}
	return client.newJavaStringArray(names)
}

// databaseExists reports whether the title has a database: its container is
// there, or an object holds its records with a write the store refused.
func (client *Client) databaseExists(name string) bool {
	if _, stored := client.readDatabase(name); stored {
		return true
	}
	content := client.openDatabaseContent(name)
	return content != nil && content.unsaved
}

// databaseRecordBytes is what one database's records hold, for the space
// count: what an object on it holds, or what its container holds, read once
// per change of it.
func (client *Client) databaseRecordBytes(name string) int64 {
	if client.javaRun != nil {
		for _, database := range client.javaRun.databases {
			if database != nil && database.name == name && database.databaseContent != nil {
				return int64(database.recordBytes())
			}
		}
		if content := client.openDatabaseContent(name); content != nil {
			return int64(content.recordBytes())
		}
	}
	key, epoch, keyed := client.databaseRecordKey(name)
	if !keyed {
		return 0
	}
	known, ok := client.storedSizes[key]
	if ok && known.epoch == epoch && known.recordsKnown {
		return int64(known.records)
	}
	records := 0
	if data, stored := client.readDatabase(name); stored {
		decoded := &databaseContent{}
		if decoded.decode(data) == nil {
			records = decoded.recordBytes()
		}
	}
	if !ok || known.epoch != epoch {
		known = storedSize{epoch: epoch}
	}
	known.recordsKnown, known.records = true, records
	if client.storedSizes == nil {
		client.storedSizes = make(map[string]storedSize)
	}
	client.storedSizes[key] = known
	return int64(records)
}

// databaseIndexName is the entry earlier builds kept the names in, at the
// root of the file scope (fileDatabaseIndexKey), and a session whose store
// has refused the move still does. It carries the suffix so that it reads as
// a database's container, and **it is the platform's, not a guest file**:
// fileSaveKey gives no title's path a key there, and openDataBase refuses the
// one database name whose container it would be. Builds before those read and
// wrote it as a guest path, so a title's file of that name was the index, a
// database of that name wrote its records over it, and the created list named
// it as a file.
const databaseIndexName = "__databases__" + databaseSuffix

// loadDatabaseIndex reads the index from the store, or from what a session
// with no store keeps, in the session's layout (databaseIndexSaveKey).
// Neither an open file, the removal list nor the archive answers for it,
// because it is no guest path.
func (client *Client) loadDatabaseIndex() ([]byte, bool) {
	key := client.databaseIndexSaveKey()
	if client.saveStore != nil {
		if data, ok := client.loadSave(key); ok {
			return data, true
		}
	}
	data, kept := client.sessionFiles[key]
	return bytes.Clone(data), kept
}

// storeDatabaseIndex writes the index. A refusal keeps it waiting here, so the
// names stay what the session made them: the next create or delete starts
// from the waiting index rather than from the store's, which did not have the
// refused name, and the ending and a quick step write it again
// (storeWaitingDatabaseIndex).
func (client *Client) storeDatabaseIndex(data []byte) {
	if client.saveReadError != nil {
		return
	}
	if err := client.storeSessionSave(client.databaseIndexSaveKey(), data); err != nil {
		if client.logger != nil {
			client.logger.Debug("LGT database index store failed", "error", err)
		}
		client.databaseIndexWaiting, client.databaseIndexPending = true, bytes.Clone(data)
		return
	}
	client.databaseIndexWaiting, client.databaseIndexPending = false, nil
}

// storeWaitingDatabaseIndex gives the store an index whose last write it
// refused, and reports whether it is still waiting.
func (client *Client) storeWaitingDatabaseIndex() bool {
	if client.databaseIndexWaiting {
		client.storeDatabaseIndex(client.databaseIndexPending)
	}
	return client.databaseIndexWaiting
}

func (client *Client) databaseNames() []string {
	data, ok := client.databaseIndexPending, client.databaseIndexWaiting
	if !ok {
		data, ok = client.loadDatabaseIndex()
	}
	if !ok {
		return nil
	}
	// A line is a name exactly as it was written: a name may begin or end
	// with a space, as a file's may, and trimming the line made such a
	// database one listDataBases left out.
	names := make([]string, 0, 8)
	for _, name := range strings.Split(string(data), "\n") {
		if name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// databaseContainers answers the paths, in fileListKey's form, of the
// containers of the databases the index names, in a session whose databases
// are still kept among the files because its store refused the move
// (java_database_store.go); in their own scope no file path is a container,
// and it answers none.
//
// **A container is the runtime's own storage, not a file of the title's**
// (docs/wipi-storage.md). So no file call, C or Java, sees one while it is
// among the files: not a listing, an existence or kind question, an open, a
// removal or a rename, and not the directory a container in one would
// otherwise make. The space count still takes it once, as the database it is.
// These paths are a filter on what the file calls see. A listing, and a
// question about a name in a container's form, read the index once, as
// listDataBases does.
func (client *Client) databaseContainers() map[string]bool {
	if !client.databasesAmongFiles() {
		return nil
	}
	names := client.databaseNames()
	if len(names) == 0 {
		return nil
	}
	containers := make(map[string]bool, len(names))
	for _, name := range names {
		containers[fileListKey(databaseFileName(name))] = true
	}
	return containers
}

// databaseContainer reports whether a path is the container of a database the
// index names (databaseContainers). Only a name ending as a container does is
// looked up, so no other name costs a read of the index.
func (client *Client) databaseContainer(name string) bool {
	key := fileListKey(name)
	return strings.HasSuffix(key, databaseSuffix) && client.databaseContainers()[key]
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
	client.storeDatabaseIndex([]byte(strings.Join(kept, "\n")))
}

// javaDeleteDataBase is `deleteDataBase`, with or without the access flag. A
// database that is not there is a DataBaseException.
func javaDeleteDataBase(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	name, err := client.javaDatabaseName(thread, arguments[0])
	if err != nil {
		return 0, err
	}
	if !client.databaseExists(name) {
		return 0, client.throwJavaPlatform(thread, javaDataBaseExceptionClass,
			fmt.Sprintf(": %q does not exist", name))
	}
	shared := client.openDatabaseContent(name)
	// A removal the store refuses leaves the database as it was, as it leaves
	// a file (removeFile), and is the DataBaseException the call declares.
	if err := client.deleteDatabase(name); err != nil {
		return 0, client.throwJavaPlatform(thread, javaDataBaseExceptionClass,
			fmt.Sprintf(": %q could not be deleted: %v", name, err))
	}
	// A write the store refused goes with the database, as a write waiting on
	// a removed file goes with the file: neither a close nor the ending may
	// bring the database back with it. The objects still on it read it empty,
	// as they would once they read the deleted container again, and a write
	// through one of them makes the database again (saveDatabase).
	if shared != nil {
		shared.records, shared.deleted = nil, nil
		shared.unsaved, shared.synced = false, client.databaseEpoch(name)
		shared.unlisted = true
	}
	client.recordDatabaseName(name, false)
	return 0, nil
}

// javaDataBaseAccessMode is `getAccessMode`: the area a database is in, which
// is FileSystem.PRIVATE_ACCESS for every database a title has here, and a
// DataBaseException for one it does not have.
func javaDataBaseAccessMode(
	client *Client, _ context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	name, err := client.javaDatabaseName(thread, arguments[0])
	if err != nil {
		return 0, err
	}
	if !client.databaseExists(name) {
		return 0, client.throwJavaPlatform(thread, javaDataBaseExceptionClass,
			fmt.Sprintf(": %q does not exist", name))
	}
	return javaPrivateAccess, nil
}
