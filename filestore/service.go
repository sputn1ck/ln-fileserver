package filestore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	uuid "github.com/satori/go.uuid"
	"io"
	"os"
	"path/filepath"
	"time"
)


type UserConfigStore interface {
	Create(ctx context.Context, pubkey string) (*UserConfig, error)
	Read(ctx context.Context, pubkey string) (*UserConfig, error)
	Update (ctx context.Context, config *UserConfig) error
}
type Service struct{
	store UserConfigStore
	baseDir string
}

func NewService(store UserConfigStore, baseDir string) (*Service, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, err
	}
	return &Service{store: store, baseDir: baseDir}, nil
}

func (s *Service) ListFiles(ctx context.Context, pubkey string) (map[string]*FileSlot, error) {
	userConfig, err := s.store.Read(ctx, pubkey)
	if err != nil {
		return nil, err
	}
	return userConfig.FileSlots, nil
}

func (s *Service) GetFileReader(ctx context.Context, pubkey string, fileid string) (*os.File, error) {
	f, err := os.Open(filepath.Join(s.baseDir, pubkey, fileid))
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (s *Service) GetFileWriter(ctx context.Context, pubkey string, fileid string) (*os.File, error) {
	f, err := os.Create(filepath.Join(s.baseDir, pubkey, fileid))
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (s *Service) NewFile(ctx context.Context, pubkey string, filename string, description string, deleteAt int64) (*FileSlot, error) {
	_, err := s.store.Read(ctx, pubkey)
	if err == NotFoundErr {
		_, err = s.store.Create(ctx, pubkey)
		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	return &FileSlot{
		FileName:     filename,
		Description:  description,
		DeletionDate: deleteAt,
		Id: uuid.NewV4().String(),
	}, nil

}

func (s *Service) SaveFile(ctx context.Context, pubkey string, slot *FileSlot, file *os.File) (*FileSlot, error) {
	// Get User Config
	userConfig, err := s.store.Read(ctx, pubkey)
	if err != nil {
		return nil, err
	}
	// set Sha hash
	_,err = file.Seek(0, io.SeekStart)
	if err != nil {
		return nil, err
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return nil, err
	}
	slot.Sha256Checksum = hex.EncodeToString(hasher.Sum(nil))
	// set bytes
	fi, err := file.Stat()
	if err != nil {
		return nil, err
	}
	slot.Bytes = fi.Size()
	// set creation date
	slot.CreationDate = time.Now().UTC().Unix()
	userConfig.FileSlots[slot.Id] = slot

	err = s.store.Update(ctx, userConfig)
	if err != nil {
		return nil, err
	}
	return slot, nil
}

func (s *Service) GetFile(ctx context.Context, pubkey string, fileid string) (*FileSlot ,error) {
	// Get User Config
	userConfig, err := s.store.Read(ctx, pubkey)
	if err != nil {
		return nil, err
	}
	if val, ok := userConfig.FileSlots[fileid]; ok {
		return val, nil
	}
	return nil, fmt.Errorf("File not found or user does not own file")
}