package db

import (
	"database/sql"
	"encoding/hex"
	"errors"
	"github.com/textileio/textile-go/repo"
	"github.com/textileio/textile-go/repo/wallet"
	"github.com/btcsuite/btcd/btcec"
	"strconv"
	"sync"
)

type KeysDB struct {
	modelStore
}

func NewKeyStore(db *sql.DB, lock *sync.Mutex) repo.KeyStore {
	return &KeysDB{modelStore{db, lock}}
}

func (k *KeysDB) Put(scriptAddress []byte, keyPath wallet.KeyPath) error {
	k.lock.Lock()
	defer k.lock.Unlock()
	tx, err := k.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare("insert into keys(scriptAddress, purpose, keyIndex, used) values(?,?,?,?)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	_, err = stmt.Exec(hex.EncodeToString(scriptAddress), int(keyPath.Purpose), keyPath.Index, 0)
	if err != nil {
		tx.Rollback()
		return err
	}
	tx.Commit()
	return nil
}

func (k *KeysDB) ImportKey(scriptAddress []byte, key *btcec.PrivateKey) error {
	k.lock.Lock()
	defer k.lock.Unlock()
	tx, err := k.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare("insert into keys(scriptAddress, purpose, used, key) values(?,?,?,?)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	_, err = stmt.Exec(hex.EncodeToString(scriptAddress), -1, 0, hex.EncodeToString(key.Serialize()))
	if err != nil {
		tx.Rollback()
		return err
	}
	tx.Commit()
	return nil
}

func (k *KeysDB) MarkKeyAsUsed(scriptAddress []byte) error {
	k.lock.Lock()
	defer k.lock.Unlock()
	tx, err := k.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare("update keys set used=1 where scriptAddress=?")
	if err != nil {
		return err
	}

	defer stmt.Close()
	_, err = stmt.Exec(hex.EncodeToString(scriptAddress))
	if err != nil {
		tx.Rollback()
		return err
	}
	tx.Commit()
	return nil
}

func (k *KeysDB) GetLastKeyIndex(purpose wallet.KeyPurpose) (int, bool, error) {
	k.lock.Lock()
	defer k.lock.Unlock()

	stm := "select keyIndex, used from keys where purpose=" + strconv.Itoa(int(purpose)) + " order by rowid desc limit 1"
	stmt, err := k.db.Prepare(stm)
	defer stmt.Close()
	var index int
	var usedInt int
	err = stmt.QueryRow().Scan(&index, &usedInt)
	if err != nil {
		return 0, false, err
	}
	var used bool
	if usedInt == 0 {
		used = false
	} else {
		used = true
	}
	return index, used, nil
}

func (k *KeysDB) GetPathForKey(scriptAddress []byte) (wallet.KeyPath, error) {
	k.lock.Lock()
	defer k.lock.Unlock()

	stmt, err := k.db.Prepare("select purpose, keyIndex from keys where scriptAddress=?")
	if err != nil {
		return wallet.KeyPath{}, err
	}
	defer stmt.Close()
	var purpose int
	var index int
	err = stmt.QueryRow(hex.EncodeToString(scriptAddress)).Scan(&purpose, &index)
	if err != nil {
		return wallet.KeyPath{}, errors.New("Key not found")
	}
	p := wallet.KeyPath{
		Purpose: wallet.KeyPurpose(purpose),
		Index:   index,
	}
	return p, nil
}

func (k *KeysDB) GetKey(scriptAddress []byte) (*btcec.PrivateKey, error) {
	k.lock.Lock()
	defer k.lock.Unlock()

	stmt, err := k.db.Prepare("select key from keys where scriptAddress=? and purpose=-1")
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	var keyHex string
	err = stmt.QueryRow(hex.EncodeToString(scriptAddress)).Scan(&keyHex)
	if err != nil {
		return nil, errors.New("Key not found")
	}
	keyBytes, err := hex.DecodeString(keyHex)
	if err != nil {
		return nil, err
	}
	key, _ := btcec.PrivKeyFromBytes(btcec.S256(), keyBytes)
	return key, nil
}

func (k *KeysDB) GetImported() ([]*btcec.PrivateKey, error) {
	k.lock.Lock()
	defer k.lock.Unlock()
	var ret []*btcec.PrivateKey
	stm := "select key from keys where purpose=-1"
	rows, err := k.db.Query(stm)
	if err != nil {
		return ret, err
	}
	defer rows.Close()
	for rows.Next() {
		var keyHex []byte
		err = rows.Scan(&keyHex)
		if err != nil {
			return ret, err
		}
		keyBytes, err := hex.DecodeString(string(keyHex))
		if err != nil {
			return ret, err
		}
		priv, _ := btcec.PrivKeyFromBytes(btcec.S256(), keyBytes)
		ret = append(ret, priv)
	}
	return ret, nil
}

func (k *KeysDB) GetUnused(purpose wallet.KeyPurpose) ([]int, error) {
	k.lock.Lock()
	defer k.lock.Unlock()
	var ret []int
	stm := "select keyIndex from keys where purpose=" + strconv.Itoa(int(purpose)) + " and used=0 order by rowid asc"
	rows, err := k.db.Query(stm)
	if err != nil {
		return ret, err
	}
	defer rows.Close()
	for rows.Next() {
		var index int
		err = rows.Scan(&index)
		if err != nil {
			return ret, err
		}
		ret = append(ret, index)
	}
	return ret, nil
}

func (k *KeysDB) GetAll() ([]wallet.KeyPath, error) {
	k.lock.Lock()
	defer k.lock.Unlock()
	var ret []wallet.KeyPath
	stm := "select purpose, keyIndex from keys"
	rows, err := k.db.Query(stm)
	if err != nil {
		return ret, err
	}
	defer rows.Close()
	for rows.Next() {
		var purpose int
		var index int
		if err := rows.Scan(&purpose, &index); err != nil {
			continue
		}
		p := wallet.KeyPath{
			Purpose: wallet.KeyPurpose(purpose),
			Index:   index,
		}
		ret = append(ret, p)
	}
	return ret, nil
}

func (k *KeysDB) GetLookaheadWindows() map[wallet.KeyPurpose]int {
	k.lock.Lock()
	defer k.lock.Unlock()
	windows := make(map[wallet.KeyPurpose]int)
	for i := 0; i < 2; i++ {
		stm := "select used from keys where purpose=" + strconv.Itoa(i) + " order by rowid desc"
		rows, err := k.db.Query(stm)
		if err != nil {
			continue
		}
		var unusedCount int
		for rows.Next() {
			var used int
			if err := rows.Scan(&used); err != nil {
				used = 1
			}
			if used == 0 {
				unusedCount++
			} else {
				break
			}
		}
		purpose := wallet.KeyPurpose(i)
		windows[purpose] = unusedCount
		rows.Close()
	}
	return windows
}