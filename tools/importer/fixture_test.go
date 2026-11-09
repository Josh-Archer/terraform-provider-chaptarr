package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func fakeCanary(name string) string {
	return strings.Join([]string{"canary", name, "not-real"}, "-")
}

type apiCanaries struct {
	hostPassword   string
	hostAPIKey     string
	proxyPassword  string
	rootPassword   string
	indexerKey     string
	downloadSecret string
	notification   string
	importListKey  string
	metadataKey    string
	hardcoverToken string
}

func (c apiCanaries) values() []string {
	return []string{
		c.hostPassword, c.hostAPIKey, c.proxyPassword, c.rootPassword, c.indexerKey,
		c.downloadSecret, c.notification, c.importListKey, c.metadataKey, c.hardcoverToken,
	}
}

func testCanaries() apiCanaries {
	return apiCanaries{
		hostPassword:   fakeCanary("host-password"),
		hostAPIKey:     fakeCanary("host-api-key"),
		proxyPassword:  fakeCanary("proxy-password"),
		rootPassword:   fakeCanary("root-password"),
		indexerKey:     fakeCanary("indexer-api-key"),
		downloadSecret: fakeCanary("download-password"),
		notification:   fakeCanary("notification-token"),
		importListKey:  fakeCanary("list-api-key"),
		metadataKey:    fakeCanary("metadata-api-key"),
		hardcoverToken: fakeCanary("hardcover-token"),
	}
}

type recordedCall struct {
	Method string
	Path   string
	Query  string
	APIKey string
}

type populatedServer struct {
	URL      string
	Canaries apiCanaries
	server   *httptest.Server
	mu       sync.Mutex
	calls    []recordedCall
}

func (s *populatedServer) Close() { s.server.Close() }

func (s *populatedServer) Calls() []recordedCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]recordedCall, len(s.calls))
	copy(out, s.calls)
	return out
}

func (s *populatedServer) Reset() {
	s.mu.Lock()
	s.calls = nil
	s.mu.Unlock()
}

func newPopulatedChaptarr(t *testing.T) *populatedServer {
	t.Helper()
	secrets := testCanaries()
	docs := map[string]any{}
	addObject := func(path string, id int, doc map[string]any) {
		doc["id"] = id
		docs[path] = doc
		docs[fmt.Sprintf("%s/%d", path, id)] = doc
	}
	addList := func(path string, items ...map[string]any) {
		docs[path] = items
		for _, item := range items {
			docs[fmt.Sprintf("%s/%d", path, item["id"].(int))] = item
		}
	}
	schema := func(path, implementation, contract string, fields []any) {
		docs[path] = []any{map[string]any{
			"name": implementation, "implementation": implementation, "configContract": contract, "fields": fields,
		}}
	}

	addObject("/api/v1/config/host", 1, map[string]any{
		"bindAddress": "*", "port": 8787, "username": "admin",
		"password": secrets.hostPassword, "apiKey": secrets.hostAPIKey,
	})
	addObject("/api/v1/config/ui", 1, map[string]any{"theme": "dark"})
	addObject("/api/v1/config/mediamanagement", 1, map[string]any{"copyUsingHardlinks": true})
	addObject("/api/v1/config/naming", 1, map[string]any{"renameBooks": true})
	addObject("/api/v1/config/conversion", 1, map[string]any{"audiobookConversionConcurrentConversions": 1})
	addObject("/api/v1/config/metadataprovider", 1, map[string]any{"embedMetadata": true})
	addObject("/api/v1/config/downloadclient", 1, map[string]any{"enableCompletedDownloadHandling": true})
	addObject("/api/v1/config/indexer", 1, map[string]any{"rssSyncInterval": 15})
	addObject("/api/v1/config/development", 1, map[string]any{"consoleLogLevel": "info"})
	docs["/api/v1/config/hardcover"] = map[string]any{"enabled": true, "username": "reader", "token": secrets.hardcoverToken}

	addList("/api/v1/tag", map[string]any{"id": 7, "label": "fiction"})
	addList("/api/v1/importlistexclusion", map[string]any{"id": 3, "foreignId": "hc:999", "authorName": "Skipped", "mediaType": ""})
	addList("/api/v1/customfilter", map[string]any{"id": 6, "type": "book", "label": "Wanted", "filters": []any{}})
	addList("/api/v1/customformat", map[string]any{
		"id": 9, "name": "M4B", "appliesTo": "audiobook", "includeCustomFormatWhenRenaming": true,
		"specifications": []any{map[string]any{"name": "Release", "implementation": "ReleaseTitleSpecification"}},
	})
	addList("/api/v1/qualityprofile", map[string]any{
		"id": 2, "name": "Spoken", "profileType": "audiobook", "upgradeAllowed": true,
		"preferCustomFormatsOverQuality": false, "cutoff": 2, "minFormatScore": 0, "cutoffFormatScore": 0,
		"items": []any{
			map[string]any{
				"id": 100, "name": "Lossless", "allowed": true,
				"items": []any{
					map[string]any{"id": 101, "name": "FLAC", "allowed": true, "quality": map[string]any{"id": 2, "name": "FLAC"}},
					map[string]any{"id": 102, "name": "MP3", "allowed": false, "quality": map[string]any{"id": 3, "name": "MP3"}},
				},
			},
			map[string]any{"id": 11, "name": "EPUB", "allowed": true, "quality": map[string]any{"id": 5, "name": "EPUB"}, "items": []any{}},
		},
		"formatItems": []any{map[string]any{"format": 9, "name": "M4B", "score": 10, "builtInKey": "m4b"}},
	})
	addList("/api/v1/metadataprofile", map[string]any{
		"id": 5, "name": "Standard", "profileType": 1, "minPopularity": 0, "minPages": 0,
		"allowedLanguages": "en,de", "ignored": []any{"Omnibus"},
	})
	addList("/api/v1/releaseprofile", map[string]any{
		"id": 4, "enabled": true, "required": []any{"m4b"}, "ignored": []any{"retail"}, "indexerId": 1, "tags": []any{7},
	})
	addList("/api/v1/delayprofile", map[string]any{
		"id": 1, "enableUsenet": true, "enableTorrent": false, "preferredProtocol": "usenet",
		"usenetDelay": 0, "torrentDelay": 0, "bypassIfHighestQuality": false,
		"bypassIfAboveCustomFormatScore": false, "minimumCustomFormatScore": 0, "order": 1, "tags": []any{7},
	})
	addList("/api/v1/qualitydefinition", map[string]any{
		"id": 8, "quality": map[string]any{"id": 4, "name": "FLAC"}, "title": "FLAC",
		"minSize": 20.5, "maxSize": 350.5,
	})
	addList("/api/v1/settings/proxy", map[string]any{
		"id": 3, "name": "Local", "type": "http", "hostname": "proxy.local", "port": 8080,
		"username": "proxyuser", "password": secrets.proxyPassword,
	})
	addList("/api/v1/rootfolder", map[string]any{
		"id": 15, "name": "Audio", "path": "/audio", "folderType": 1, "isCalibreLibrary": true,
		"host": "calibre.local", "port": 8080, "username": "calibre", "password": secrets.rootPassword,
		"library": "Main", "outputProfile": "default", "useSsl": false, "placeEbooksWithAudiobooks": false,
		"audiobookQualityProfileId": 2, "audiobookMetadataProfileId": 5, "defaultTags": []any{7},
	})
	addList("/api/v1/remotepathmapping", map[string]any{
		"id": 16, "host": "downloader", "downloadClientId": 0, "remotePath": "/downloads", "localPath": "/audio",
	})
	addList("/api/v1/metadata", map[string]any{
		"id": 34, "name": "Shelf", "implementation": "AudioBookshelf", "configContract": "AudioBookshelfSettings",
		"enable": false, "tags": []any{},
		"fields": []any{
			map[string]any{"name": "host", "value": "shelf.local"},
			map[string]any{"name": "apiKey", "value": secrets.metadataKey, "privacy": "apiKey"},
		},
	})
	addList("/api/v1/indexer", map[string]any{
		"id": 30, "name": "Prowlarr", "implementation": "Prowlarr", "configContract": "ProwlarrSettings",
		"enable": false, "enableRss": true, "enableAutomaticSearch": false, "enableInteractiveSearch": false,
		"priority": 25, "downloadClientId": 0, "tags": []any{7},
		"fields": []any{
			map[string]any{"name": "baseUrl", "value": "http://indexer.local"},
			map[string]any{"name": "apiKey", "value": secrets.indexerKey, "privacy": "apiKey"},
		},
	})
	addList("/api/v1/downloadclient", map[string]any{
		"id": 31, "name": "qBittorrent", "implementation": "QBittorrent", "configContract": "QBittorrentSettings",
		"enable": false, "protocol": "torrent", "priority": 1, "tags": []any{99},
		"removeCompletedDownloads": false, "removeFailedDownloads": false, "copyUnmanagedDownloads": false,
		"audiobookTags": []any{7},
		"fields": []any{
			map[string]any{"name": "host", "value": "downloader.local"},
			map[string]any{"name": "password", "value": secrets.downloadSecret, "privacy": "password", "type": "password"},
		},
	})
	addList("/api/v1/notification", map[string]any{
		"id": 32, "name": "Ntfy", "implementation": "Ntfy", "configContract": "NtfySettings", "enable": false,
		"onGrab": true,
		"fields": []any{
			map[string]any{"name": "server", "value": "https://ntfy.local"},
			map[string]any{"name": "token", "value": secrets.notification, "type": "password"},
		},
	})
	addList("/api/v1/importlist", map[string]any{
		"id": 33, "name": "Program List", "implementation": "CustomList", "configContract": "CustomListSettings",
		"enable": false, "enableAutomaticAdd": false, "shouldMonitor": "none", "shouldMonitorExisting": false,
		"shouldSearch": false, "rootFolderPath": "/audio", "monitorNewItems": "all",
		"qualityProfileId": 2, "metadataProfileId": 5, "listType": "program",
		"fields": []any{
			map[string]any{"name": "userId", "value": "reader"},
			map[string]any{"name": "apiKey", "value": secrets.importListKey, "privacy": "apiKey"},
		},
	})
	addList("/api/v1/author", map[string]any{
		"id": 41, "foreignAuthorId": "hc:191785", "authorName": "Example Author", "monitored": true,
		"audiobookMonitorExisting": 0, "audiobookMonitorFuture": false, "ebookMonitorExisting": 0, "ebookMonitorFuture": false,
		"audiobookRootFolderPath": "/audio", "audiobookQualityProfileId": 2, "audiobookMetadataProfileId": 5,
		"audiobookTags": []any{7},
	})
	addList("/api/v1/series", map[string]any{
		"id": 12, "foreignSeriesId": "hc:555", "title": "Example Series", "mediaType": "audiobook", "narrator": "Example Narrator",
	})
	book := map[string]any{
		"id": 20, "foreignBookId": "hc:777", "authorId": 41, "title": "Example Book", "mediaType": "audiobook",
		"monitored": true, "anyEditionOk": false,
		"editions": []any{map[string]any{"id": 21, "foreignEditionId": "hc:ed1", "title": "First Edition", "format": "audiobook", "monitored": true}},
	}
	addList("/api/v1/book", book)
	docs["/api/v1/edition?bookId=20"] = book["editions"]

	schema("/api/v1/indexer/schema", "Prowlarr", "ProwlarrSettings", []any{
		map[string]any{"name": "baseUrl"}, map[string]any{"name": "apiKey", "privacy": "apiKey"},
	})
	schema("/api/v1/downloadclient/schema", "QBittorrent", "QBittorrentSettings", []any{
		map[string]any{"name": "host"}, map[string]any{"name": "password", "privacy": "password", "type": "password"},
	})
	schema("/api/v1/notification/schema", "Ntfy", "NtfySettings", []any{
		map[string]any{"name": "server"}, map[string]any{"name": "token", "type": "password"},
	})
	schema("/api/v1/importlist/schema", "CustomList", "CustomListSettings", []any{
		map[string]any{"name": "userId"}, map[string]any{"name": "apiKey", "privacy": "apiKey"},
	})
	schema("/api/v1/metadata/schema", "AudioBookshelf", "AudioBookshelfSettings", []any{
		map[string]any{"name": "host"}, map[string]any{"name": "apiKey", "privacy": "apiKey"},
	})

	live := &populatedServer{Canaries: secrets}
	live.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		live.mu.Lock()
		live.calls = append(live.calls, recordedCall{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, APIKey: r.Header.Get("X-Api-Key")})
		live.mu.Unlock()
		if strings.Contains(r.URL.Path, "preserved") {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodPost || r.Method == http.MethodDelete {
			http.Error(w, "unexpected mutation", http.StatusInternalServerError)
			return
		}
		if r.Method == http.MethodPut {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("{}\n"))
			return
		}
		key := r.URL.Path
		if r.URL.RawQuery != "" {
			key += "?" + r.URL.RawQuery
		}
		doc, ok := docs[key]
		if !ok {
			doc, ok = docs[r.URL.Path]
		}
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(doc); err != nil {
			http.Error(w, "encode", http.StatusInternalServerError)
		}
	}))
	live.URL = live.server.URL
	return live
}
