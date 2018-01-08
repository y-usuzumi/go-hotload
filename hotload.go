package hotload

import (
	"github.com/fsnotify/fsnotify"
	"io/ioutil"
	"log"
	"os"
	"sync"
	"time"
)

type Method uint8

const (
	Inotify = iota
	Poll
)

type LoadFunc func(configContent []byte) (interface{}, error)
type ObserverFunc func(interface{})

type HotLoader struct {
	mux        sync.Mutex
	writeMux   sync.Mutex
	emptyCond  sync.Cond
	watcherMux sync.Mutex
	config     interface{}
	watchers   []func(interface{})
	loader     LoadFunc
}

func CreateHotLoader(loader LoadFunc) *HotLoader {
	return &HotLoader{loader: loader, emptyCond: *sync.NewCond(&sync.Mutex{})}
}

func (loader *HotLoader) Get() interface{} {
	if loader.config == nil {
		loader.emptyCond.L.Lock()
		loader.emptyCond.Wait()
		loader.emptyCond.L.Unlock()
		log.Println("Yoy")
	}
	return loader.config
}

func (loader *HotLoader) load(path string) (interface{}, error) {
	loader.writeMux.Lock()
	defer loader.writeMux.Unlock()
	content, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	config, err := loader.loader(content)
	if err != nil {
		return nil, err
	}
	needBroadcast := false
	if loader.config == nil {
		needBroadcast = true
	}
	loader.config = config
	if needBroadcast {
		loader.emptyCond.Broadcast()
	}
	for _, watcher := range loader.watchers {
		watcher(config)
	}
	return config, nil
}

func (loader *HotLoader) OnUpdate(f ObserverFunc) {
	loader.watcherMux.Lock()
	defer loader.watcherMux.Unlock()
	loader.watchers = append(loader.watchers, f)
}

func (loader *HotLoader) Watch(method Method, path string) {
	_, err := loader.load(path)
	if err != nil {
		log.Println("Config load error. Please correct the config file")
	}
	switch method {
	case Inotify:
		loader.watchInotify(path)
	case Poll:
		loader.watchPoll(path)
	default:
		log.Fatal("Unsupported watch method: ", method)
	}
}

func (loader *HotLoader) watchInotify(path string) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal("Error: ", err)
		os.Exit(3)
	}
	watcher.Add(path)
	go func() {
		log.Println("Watching file")
		for {
			select {
			case event := <-watcher.Events:
				if event.Op&fsnotify.Write > 0 {
					_, err := loader.load(path)
					if err != nil {
						log.Println("Error: ", err)
						log.Println("Config load error. Use old config")
					}
				}
			case err := <-watcher.Errors:
				log.Println("Error: ", err)
			}
		}
	}()
}

func (loader *HotLoader) watchPoll(path string) {
	go func() {
		log.Println("Watching file")
		fi, err := os.Stat(path)
		if err != nil {
			log.Println("Error: ", err)
		}
		for {
			newfi, err := os.Stat(path)
			if err != nil {
				log.Println("Error: ", err)
			} else if newfi.ModTime() != fi.ModTime() {
				fi = newfi
				_, err := loader.load(path)
				if err != nil {
					log.Println("Error: ", err)
				}
			}
			time.Sleep(1 * time.Second)
		}
	}()
}
