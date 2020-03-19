package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/alecthomas/jsonschema"
	ipfslite "github.com/hsanjuan/ipfs-lite"
	"github.com/ipfs/go-cid"
	"github.com/mr-tron/base58"
	core "github.com/textileio/go-threads/core/db"
	"github.com/textileio/go-threads/core/thread"
	"github.com/textileio/go-threads/db"
	"github.com/textileio/go-threads/examples/foldersync/watcher"
)

var (
	errClientAlreadyStarted = errors.New("client already started")
	collectionName          = "sharedFolder"
	collectionDummyInstance = &userFolder{}
)

type client struct {
	lock          sync.Mutex
	closeIpfsLite func() error
	wg            sync.WaitGroup
	closeCh       chan struct{}
	started       bool
	closed        bool

	shrFolderPath  string
	userName       string
	folderInstance *userFolder

	net        db.NetBoostrapper
	db         *db.DB
	collection *db.Collection
	peer       *ipfslite.Peer
}

type userFolder struct {
	ID    core.InstanceID
	Owner string
	Files []file
}

func (u *userFolder) toJSONString() (string, error) {
	bytes, err := json.Marshal(u)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

type file struct {
	ID               core.InstanceID
	FileRelativePath string
	CID              string

	IsDirectory bool
	Files       []file
}

func newClient(name, sharedFolderPath, repoPath string, inviteLink string) (*client, error) {
	net, err := db.DefaultNetwork(repoPath)
	if err != nil {
		return nil, err
	}

	ipfspeer := net.GetIpfsLite()
	if err != nil {
		return nil, fmt.Errorf("error when creating ipfs lite peer: %v", err)
	}

	threadID := thread.NewIDV1(thread.Raw, 32)
	var d *db.DB
	var collection *db.Collection

	if inviteLink == "" {
		log.Infof("Creating a new DB with thread id: %v", threadID.String())
		d, err = db.NewDB(context.Background(), net, threadID, db.WithRepoPath(repoPath), db.WithJsonMode(true))
		if err != nil {
			return nil, fmt.Errorf("error when creating db: %v", err)
		}

		schema, err := getSchema()
		if err != nil {
			return nil, err
		}

		log.Infof("Creating sharedFolder collection")
		collecitonConf := db.CollectionConfig{
			Name:   collectionName,
			Schema: schema,
		}
		collection, err = d.NewCollection(collecitonConf)
		if err != nil {
			return nil, fmt.Errorf("error when creating new collection from instance: %v", err)
		}
	} else {
		log.Infof("Creating a DB from invite: %v", inviteLink)
		addr, fk, rk := parseInviteLink(inviteLink)

		schema, err := getSchema()
		if err != nil {
			return nil, err
		}

		collecitonConf := db.CollectionConfig{
			Name:   collectionName,
			Schema: schema,
		}

		d, err = db.NewDBFromAddr(context.Background(), net, addr, fk, rk, db.WithCollections(collecitonConf), db.WithRepoPath(repoPath), db.WithJsonMode(true))
		if err != nil {
			return nil, fmt.Errorf("error when creating db from addr: %v", err)
		}

		collection = d.GetCollection(collectionName)
	}

	return &client{
		closeIpfsLite: func() error { return nil },
		closeCh:       make(chan struct{}),
		net:           net,
		peer:          ipfspeer,
		shrFolderPath: sharedFolderPath,
		userName:      name,
		db:            d,
		collection:    collection,
	}, nil
}

func getSchema() (string, error) {
	schema := jsonschema.Reflect(collectionDummyInstance)
	schemaBytes, err := json.Marshal(schema)
	if err != nil {
		return "", fmt.Errorf("error when creating json schema from instance: %v", err)
	}
	return string(schemaBytes), nil
}

func (c *client) close() error {
	c.lock.Lock()
	defer c.lock.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true

	close(c.closeCh)
	c.wg.Wait()
	if err := c.closeIpfsLite(); err != nil {
		return err
	}
	c.db.Close()
	if err := c.net.Close(); err != nil {
		return err
	}

	return nil
}

func (c *client) start() error {
	c.lock.Lock()
	defer c.lock.Unlock()
	if c.started {
		return errClientAlreadyStarted
	}

	c.wg.Add(2)
	if err := c.startFileSystemWatcher(); err != nil {
		return err
	}
	if err := c.startListeningExternalChanges(); err != nil {
		return err
	}
	c.started = true

	return nil
}

func (c *client) getDirectoryTree() ([]*userFolder, error) {
	ret, err := c.collection.FindJSON(&db.JSONQuery{})
	log.Infof("got tree: %v", ret)
	if err != nil {
		return nil, err
	}
	res := make([]*userFolder, len(ret))
	for i, jsonString := range ret {
		folder := &userFolder{}
		if err := json.Unmarshal([]byte(jsonString), folder); err != nil {
			return nil, err
		}
		res[i] = folder
	}
	return res, nil
}

func (c *client) inviteLinks() ([]string, error) {
	addrs, _, followKey, readKey, err := c.db.GetInfo()
	if err != nil {
		return make([]string, 0), err
	}
	res := make([]string, len(addrs))
	for i := range addrs {
		addr := addrs[i].String()
		fKey := base58.Encode(followKey.Bytes())
		rKey := base58.Encode(readKey.Bytes())

		res[i] = addr + "?" + fKey + "&" + rKey
	}
	return res, nil
}

func (c *client) fullPath(f file) string {
	return filepath.Join(c.shrFolderPath, f.FileRelativePath)
}

func (c *client) startListeningExternalChanges() error {
	l, err := c.db.Listen()
	if err != nil {
		return err
	}
	go func() {
		defer c.wg.Done()
		defer l.Close()
		for {
			select {
			case <-c.closeCh:
				log.Info("shutting down external changes listener")
				return
			case a := <-l.Channel():
				var ufString string
				if err := c.collection.FindByID(a.ID, &ufString); err != nil {
					log.Errorf("error when getting changed user folder with ID %s", a.ID)
					continue
				}
				var uf userFolder
				if err := json.Unmarshal([]byte(ufString), &uf); err != nil {
					log.Errorf("error unmarshalling userFolder string for id %v", a.ID)
					continue
				}
				log.Infof("%s: detected new file %s of user %s", c.userName, a.ID, uf.Owner)
				for _, f := range uf.Files {
					if err := c.ensureCID(c.fullPath(f), f.CID); err != nil {
						log.Warningf("%s: error ensuring file %s: %v", c.userName, c.fullPath(f), err)
					}
				}
			}
		}
	}()
	return nil
}

func (c *client) startFileSystemWatcher() error {
	myFolderPath := path.Join(c.shrFolderPath, c.userName)
	myFolder, err := c.getOrCreateMyFolderInstance(myFolderPath)
	if err != nil {
		return fmt.Errorf("error when getting client folder instance: %v", err)
	}
	c.folderInstance = myFolder

	watcher, err := watcher.New(myFolderPath, func(fileName string) error {
		f, err := os.Open(fileName)
		if err != nil {
			return err
		}
		n, err := c.peer.AddFile(context.Background(), f, nil)
		if err != nil {
			return err
		}

		fileRelPath := strings.TrimPrefix(fileName, c.shrFolderPath)
		fileRelPath = strings.TrimLeft(fileRelPath, "/")
		newFile := file{ID: core.NewInstanceID(), FileRelativePath: fileRelPath, CID: n.Cid().String(), Files: []file{}}
		c.folderInstance.Files = append(c.folderInstance.Files, newFile)
		folderInstanceString, err := c.folderInstance.toJSONString()
		if err != nil {
			return err
		}
		return c.collection.Save(&folderInstanceString)
	})

	if err != nil {
		return fmt.Errorf("error when creating folder watcher: %v", err)
	}
	watcher.Watch()
	go func() {
		<-c.closeCh
		watcher.Close()
		c.wg.Done()
	}()
	return nil
}

func (c *client) ensureFiles() error {
	res, err := c.getDirectoryTree()
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	for _, userFolder := range res {
		for _, f := range userFolder.Files {
			if err := c.ensureCID(c.fullPath(f), f.CID); err != nil {
				log.Errorf("%s: error ensuring file %s: %v", c.userName, c.fullPath(f), err)
			}
		}
	}
	wg.Wait()
	return nil
}

func (c *client) ensureCID(fullPath, cidStr string) error {
	cid, err := cid.Decode(cidStr)
	if err != nil {
		return err
	}
	_, err = os.Stat(fullPath)
	if err == nil {
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}

	log.Infof("Fetching file %s", fullPath)
	d1 := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	str, err := c.peer.GetFile(ctx, cid)
	if err != nil {
		return err
	}
	defer str.Close()
	if err := os.MkdirAll(filepath.Dir(fullPath), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(fullPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0660)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err = io.Copy(f, str); err != nil {
		return err
	}
	d2 := time.Now()
	d := d2.Sub(d1)
	log.Infof("Done fetching %s in %dms", fullPath, d.Milliseconds())
	return nil
}

func (c *client) getOrCreateMyFolderInstance(path string) (*userFolder, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err = os.MkdirAll(path, os.ModePerm); err != nil {
			return nil, err
		}
	}

	ret, err := c.collection.FindJSON(db.JSONWhere("Owner").Eq(c.userName))
	if err != nil {
		return nil, err
	}
	log.Infof("got folder instances: %v", ret)

	var myFolder = userFolder{}
	if len(ret) == 0 {
		ownFolder := &userFolder{Owner: c.userName, Files: []file{}}
		ownFolderJSON, err := ownFolder.toJSONString()
		if err != nil {
			return nil, err
		}
		if err := c.collection.Create(&ownFolderJSON); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(ownFolderJSON), &myFolder); err != nil {
			return nil, err
		}
	} else {
		if err := json.Unmarshal([]byte(ret[0]), &myFolder); err != nil {
			return nil, err
		}
	}

	return &myFolder, nil
}
