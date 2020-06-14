package filestore

import (
	"context"
	"fmt"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"os"
	"path/filepath"
)

const (
	dirPermissions = 0755
)
var (
	NotFoundErr      = fmt.Errorf("no userconfig found")
)

type UserConfig struct {
	Pubkey string                  `yaml:"pubkey"`
	FileSlots map[string]*FileSlot `yaml:"fileslots"`
}

type FileSlot struct {
	Id string
	FileName string `yaml:"filename"`
	Description string `yaml:"description"`
	Sha256Checksum string `yaml:"checksum"`
	Bytes int64 `yaml:"bytes"`
	CreationDate int64 `yaml:"creation_date"`
	DeletionDate int64 `yaml:"deletion_date"`
}

func (u *UserConfig) Save(file string) error{
	configBytes, err := yaml.Marshal(u)
	if err != nil {
		return fmt.Errorf("unable to marshal Fileslot: %v", err)
	}

	if err := ioutil.WriteFile(file, configBytes, dirPermissions); err != nil {
		return fmt.Errorf("unable to write yaml file: %v", err)
	}

	return nil
}

func (u *UserConfig) Read(file string) error{
	configBytes, err := ioutil.ReadFile(file)
	if err != nil {
		switch {
		case err == os.ErrNotExist:
			return NotFoundErr
		case os.IsNotExist(err):
			return NotFoundErr
		default:
			return err
		}
	}

	userConfig := &UserConfig{}
	if err := yaml.Unmarshal(configBytes, userConfig); err != nil {
		return err
	}

	return nil
}

type YmlUserConfigStore struct {
	baseDir string
}

func NewYmlUserConfigStore(baseDir string) *YmlUserConfigStore {
	return &YmlUserConfigStore{baseDir: baseDir}
}

func (y *YmlUserConfigStore) Create(ctx context.Context, pubkey string) (*UserConfig, error) {
	userConfig := &UserConfig{Pubkey:pubkey}
	configBytes, err := yaml.Marshal(userConfig)
	if err != nil {
		return nil, fmt.Errorf("unable to marshal Fileslot: %v", err)
	}
	err = os.Mkdir(filepath.Join(y.baseDir,userConfig.Pubkey), dirPermissions)
	if err != nil {
		return nil, err
	}
	f, err := os.Create(filepath.Join(y.baseDir,userConfig.Pubkey,"config.yml"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	_, err = f.Write(configBytes)
	if err != nil {
		return nil, err
	}
	return userConfig, nil
}
func (y *YmlUserConfigStore) Read(ctx context.Context, pubkey string) (*UserConfig, error) {
	configBytes, err := ioutil.ReadFile(filepath.Join(y.baseDir,pubkey,"config.yml"))
	if err != nil {
		switch {
		case err == os.ErrNotExist:
			return nil,NotFoundErr
		case os.IsNotExist(err):
			return nil,NotFoundErr
		default:
			return nil,err
		}
	}

	userConfig := &UserConfig{}
	if err := yaml.Unmarshal(configBytes, userConfig); err != nil {
		return nil,err
	}

	return userConfig, nil
}

func (y *YmlUserConfigStore) Update (ctx context.Context, config *UserConfig) error {
	configBytes, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("unable to marshal Fileslot: %v", err)
	}

	if err := ioutil.WriteFile(filepath.Join(y.baseDir,config.Pubkey,"config.yml"), configBytes, dirPermissions); err != nil {
		return fmt.Errorf("unable to write yaml file: %v", err)
	}

	return nil
}

