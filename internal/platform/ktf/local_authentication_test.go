package ktf

import (
	"os"
	"testing"

	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
	"github.com/movingwoo/wfeature/internal/testlocal"
)

func TestLocalSubscriberReceiptRecognition(t *testing.T) {
	path := testlocal.Value(t, "WFEATURE_KTF_RECEIPT_ARCHIVE", "a local archive")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	base := wipicrt.NewDirectorySaveStore(t.TempDir())
	if _, ok := authenticationSubscriberReceipt(base, archive, "01012349876").(*subscriberReceiptStore); !ok {
		t.Fatal("local receipt revision not recognized")
	}
}
