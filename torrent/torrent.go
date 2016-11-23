// Mgmt
// Copyright (C) 2013-2016+ James Shubin and the project contributors
// Written by James Shubin <james@shubin.ca> and the project contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package torrent

import (
	"fmt"
	"log"
	"net"
	"os"
	"path"
	"path/filepath"

	"github.com/purpleidea/mgmt/etcd"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/dht"
	"github.com/anacrolix/torrent/metainfo"
	etcdClient "github.com/coreos/etcd/clientv3"
	multierr "github.com/hashicorp/go-multierror"
)

// Service is interface to the torrent service.
type Service struct {
	client         *torrent.Client
	embdEtcd       *etcd.EmbdEtcd
	hostname       string
	nodePath       string
	dataDir        string
	nodesCancel    func()
	torrentsCancel func()
	actionChan     chan action
}

type action func()

// Exit stops the torrent service.
func (svc *Service) Exit() {
	_, err := svc.embdEtcd.Delete(svc.nodePath)
	if err != nil {
		log.Printf("torrent: Failed to delete dht node %s: %s", svc.nodePath, err.Error())
	}

	svc.nodesCancel()
	svc.torrentsCancel()
	svc.client.Close()

	close(svc.actionChan)
}

// Run starts the mainloop of the torrent service.
func (svc *Service) Run() {
	for {
		action := <-svc.actionChan
		if action == nil {
			return
		}
		action()
	}
}

func (svc *Service) nodeChangeCallback(re *etcd.RE) error {
	if re.Error() != nil {
		return nil
	}

	for _, event := range re.Response().Events {
		switch event.Type {
		case etcdClient.EventTypePut:
			svc.client.AddDHTNodes([]string{string(event.Kv.Value)})
		}
	}

	return nil
}

func (svc *Service) torrentsChangeCallback(re *etcd.RE) error {
	if re.Error() != nil {
		return nil
	}
	var reterr error
	for _, event := range re.Response().Events {
		switch event.Type {
		case etcdClient.EventTypePut:
			torrent, err := svc.client.AddMagnet(string(event.Kv.Value))
			if err != nil {
				log.Printf("Failed to get torrent from magnent link: %s Error: %s", string(event.Kv.Value), err.Error())
				reterr = multierr.Append(reterr, err)
			} else {
				log.Printf("Initiating torrent download: %s", torrent.Name())
				<-torrent.GotInfo()
				torrent.DownloadAll()
			}
		}
	}
	return reterr
}

// AddMultiple adds every directory listed as paramter as torrent
func (svc *Service) AddMultiple(paths ...string) error {
	var resErr error
	for _, path := range paths {
		err := svc.Add(path)
		if err != nil {
			log.Printf("torrent add: Failed to add '%s': %s", path, err.Error())
			resErr = multierr.Append(resErr, err)
		}
	}
	return resErr
}

func (svc *Service) Add(torrentPath string) error {
	_, err := os.Stat(torrentPath)
	if err != nil {
		return fmt.Errorf("torrent: Failed to add '%s' as torrent: %s", torrentPath, err.Error())
	}
	name := filepath.Base(torrentPath)
	if name == "" {
		return fmt.Errorf("torrent: Invalid torrent name")
	}

	mi := &metainfo.MetaInfo{
		AnnounceList: [][]string{},
		Comment:      name,
		CreatedBy:    "mgmt",
	}
	mi.SetDefaults()

	info := metainfo.Info{
		PieceLength: 256 * 1024,
	}

	if err := info.BuildFromFilePath(torrentPath); err != nil {
		return err
	}

	hash := mi.HashInfoBytes()

	_, err = svc.client.AddTorrent(mi)
	if err != nil {
		return err
	}
	svc.client.DHT().Announce(hash.HexString(), 0, false)

	svc.embdEtcd.Set(fmt.Sprintf("/dht/torrents/%s", name), mi.Magnet(name, hash).String())

	return nil
}

// NewTorrentService create a new torrent service instance.
func NewTorrentService(embdEtcd *etcd.EmbdEtcd, prefix, hostname string, isDebug bool) (svc *Service, err error) {
	svc = &Service{
		actionChan: make(chan action),
		embdEtcd:   embdEtcd,
	}
	addr, err := net.ResolveIPAddr("ip", hostname)
	if err != nil {
		log.Printf("torrent: Failed to resolve IP from hostname '%s': %s", hostname, err.Error())
		return nil, err
	}

	svc.dataDir = path.Join(prefix, "torrent", "storage")
	os.MkdirAll(svc.dataDir, 0644)

	cfg := &torrent.Config{
		DHTConfig: dht.ServerConfig{
			PublicIP: addr.IP,
		},
		Seed:    true,
		DataDir: svc.dataDir,
		Debug:   isDebug,
	}

	svc.client, err = torrent.NewClient(cfg)
	if err != nil {
		log.Printf("torrent: Failed to create torrent client: %s", err.Error())
		return nil, err
	}

	svc.nodesCancel, err = embdEtcd.AddWatcher("/dht/nodes/", svc.nodeChangeCallback, false, true, etcdClient.WithPrefix())
	if err != nil {
		log.Printf("torrent: Failed to add watcher for new nodes: %s", err.Error())
		return nil, err
	}
	svc.torrentsCancel, err = embdEtcd.AddWatcher("/dht/torrents/", svc.torrentsChangeCallback, false, true, etcdClient.WithPrefix())
	if err != nil {
		log.Printf("torrent: Failed to add watcher for new torrents: %s", err.Error())
		svc.nodesCancel()
		return nil, err
	}

	// Only interested on success here
	if values, err := embdEtcd.Get("/dht/nodes/"); err == nil {
		nodes := []string{}
		for _, v := range values {
			nodes = append(nodes, v)
		}
		svc.client.AddDHTNodes(nodes)
	}

	svc.nodePath = fmt.Sprintf("/dht/nodes/%s", svc.hostname)
	embdEtcd.Set(svc.nodePath, svc.client.ListenAddr().String())

	return
}
