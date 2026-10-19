package main

import "os"

type indexedTrackFile struct {
	path         string
	identity     trackFileIdentity
	diskIdentity trackFileIdentity
	info         os.FileInfo
}

type trackIdentityIndex struct {
	files      map[string]*indexedTrackFile
	byIdentity map[string]map[string]*indexedTrackFile
}

func (index *trackIdentityIndex) remove(path string) {
	if previous := index.files[path]; previous != nil {
		for _, key := range previous.identity.keys() {
			delete(index.byIdentity[key], path)
			if len(index.byIdentity[key]) == 0 {
				delete(index.byIdentity, key)
			}
		}
		delete(index.files, path)
	}
}

func (index *trackIdentityIndex) store(path string, identity trackFileIdentity, info os.FileInfo) *indexedTrackFile {
	index.remove(path)
	file := &indexedTrackFile{path: path, identity: identity, diskIdentity: identity, info: info}
	index.files[path] = file
	for _, key := range identity.keys() {
		if index.byIdentity[key] == nil {
			index.byIdentity[key] = make(map[string]*indexedTrackFile)
		}
		index.byIdentity[key][path] = file
	}
	return file
}

func (file *indexedTrackFile) unchanged(info os.FileInfo) bool {
	return os.SameFile(file.info, info) && file.info.Size() == info.Size() &&
		file.info.ModTime().Equal(info.ModTime())
}
