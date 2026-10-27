package models

import (
	"database/sql/driver"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
)

// UUIDBinary adalah tipe kustom yang membungkus uuid.UUID agar bisa
// dipetakan dengan kolom BINARY(16) di MySQL.
//
// Kenapa perlu tipe kustom?
//   - Di database, UUID disimpan sebagai BINARY(16) (16 byte) supaya hemat
//     ruang dan index lebih efisien dibanding CHAR(36) biasa.
//   - Di Go, kita tetap ingin memakai uuid.UUID (mudah parsing & formatting).
//   - database/sql tidak tahu cara mengonversi uuid.UUID <-> BINARY(16)
//     secara otomatis. Untuk itu tipe ini mengimplementasikan dua interface:
//       * sql.Scanner   -> Scan(),  dipakai saat membaca hasil query.
//       * driver.Valuer -> Value(), dipakai saat mengirim parameter ke DB.
//
// Dengan wrapper ini, seluruh kode repository bisa bekerja dengan
// models.UUIDBinary seolah-olah tipe native, tanpa harus manual
// marshal/unmarshal di setiap query.
type UUIDBinary uuid.UUID

// NewUUIDBinary membuat UUID v7 (time-ordered) untuk primary key.
//
// Kenapa v7, bukan v4 (random)?
// UUID v7 memiliki prefix timestamp sehingga nilainya cenderung monoton
// naik. Ini bagus untuk:
//   - Index B-tree di InnoDB: insert tidak "menyebar" random, jadi lebih
//     ramah cache dan mengurangi page split.
//   - Query ORDER BY id yang lebih predictable.
//
// uuid.Must dipakai karena pembuatan UUID v7 praktis tidak pernah error;
// kalau pun error, itu kondisi fatal yang layak panic.
func NewUUIDBinary() UUIDBinary {
	return UUIDBinary(uuid.Must(uuid.NewV7()))
}

// --- UUIDBinary di-encode/decode sebagai string UUID biasa di JSON ---

// MarshalJSON mengubah UUIDBinary menjadi string UUID di response JSON.
// Tanpa method ini, encoding/json akan menganggap UUIDBinary sebagai
// [16]byte dan mengeluarkannya sebagai array angka — tidak nyaman untuk
// konsumen API.
func (u UUIDBinary) MarshalJSON() ([]byte, error) {
	return json.Marshal(uuid.UUID(u).String())
}

// UnmarshalJSON membaca string UUID dari request body.
//
// Perilaku khusus:
//   - String kosong ("") diperlakukan sebagai UUID nil (00000000-...-0000).
//     Ini berguna saat client secara eksplisit mengirim null/empty untuk
//     field yang opsional, tanpa dianggap error parsing.
//   - Selain itu, string harus berupa UUID valid; kalau tidak, error
//     dikembalikan agar handler bisa membalas 400.
func (u *UUIDBinary) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	if s == "" {
		*u = UUIDBinary(uuid.Nil)
		return nil
	}
	parsed, err := uuid.Parse(s)
	if err != nil {
		return err
	}
	*u = UUIDBinary(parsed)
	return nil
}

// helperScan membaca data 16-byte biner dari database MySQL lalu
// mengubahnya menjadi UUID Go.
//
// Kenapa harus menangani beberapa tipe `value`?
// Driver MySQL (go-sql-driver/mysql) bisa mengembalikan kolom BINARY
// dalam bentuk:
//   - []byte  -> kasus normal untuk BINARY(16).
//   - string  -> beberapa konfigurasi/driver mengembalikan sebagai string.
//   - nil     -> kolom NULL (mis. kolom UUID opsional yang belum diisi).
//
// Dengan menangani ketiganya, model ini aman dipakai baik untuk kolom
// UUID wajib maupun opsional. Nilai NULL dipetakan ke uuid.Nil agar
// pemanggil tidak perlu cek NULL secara manual.
func helperScan(u *UUIDBinary, value interface{}) error {
	var bytes []byte
	switch v := value.(type) {
	case nil:
		*u = UUIDBinary(uuid.Nil)
		return nil
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return errors.New("gagal scan UUIDBinary: tipe data tidak dikenali")
	}

	parsedUUID, err := uuid.FromBytes(bytes)
	if err != nil {
		return err
	}
	*u = UUIDBinary(parsedUUID)
	return nil
}

// Scan mengimplementasikan sql.Scanner.
// Dipanggil otomatis oleh database/sql setiap kali kolom UUID dibaca
// dari hasil query (Row.Scan / Rows.Scan).
// Logika sebenarnya ada di helperScan; di sini hanya meneruskan.
func (u *UUIDBinary) Scan(value interface{}) error {
	return helperScan(u, value)
}

// Value mengimplementasikan driver.Valuer.
// Dipanggil otomatis oleh database/sql ketika UUIDBinary dipakai sebagai
// parameter query (INSERT/UPDATE/WHERE).
//
// Perilaku khusus:
//   - UUID nil dikembalikan sebagai nil (SQL NULL), sehingga kolom yang
//     memang opsional bisa berisi NULL, bukan 16 byte nol.
//   - Selain itu, dipakai MarshalBinary() dari uuid.UUID yang menghasilkan
//     tepat 16 byte — cocok dengan definisi kolom BINARY(16).
func (u UUIDBinary) Value() (driver.Value, error) {
	parsedUUID := uuid.UUID(u)
	if parsedUUID == uuid.Nil {
		return nil, nil
	}
	return parsedUUID.MarshalBinary()
}