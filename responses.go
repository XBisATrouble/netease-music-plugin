package main

// ---- 搜索 ----

// SearchArtistResponse 映射 /search?type=100(歌手)的响应。
type SearchArtistResponse struct {
	Code   int `json:"code"`
	Result struct {
		ArtistCount int      `json:"artistCount"`
		Artists     []Artist `json:"artists"`
	} `json:"result"`
}

// SearchAlbumResponse 映射 /search?type=10(专辑)的响应。
type SearchAlbumResponse struct {
	Code   int `json:"code"`
	Result struct {
		AlbumCount int          `json:"albumCount"`
		Albums     []AlbumBrief `json:"albums"`
	} `json:"result"`
}

// SearchSongResponse 映射 /search?type=1(单曲)的响应。
type SearchSongResponse struct {
	Code   int `json:"code"`
	Result struct {
		SongCount int       `json:"songCount"`
		Songs     []SongHit `json:"songs"`
	} `json:"result"`
}

// ---- 歌手 ----

type Artist struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	PicURL    string `json:"picUrl"`
	Img1v1URL string `json:"img1v1Url"`
}

// ArtistDescResponse 映射 /artist/desc 的响应。
// briefDesc 是一段式综述(多数歌手有,质量高,优先使用);
// 部分歌手 briefDesc 为空,详情分段落在 introduction 数组中,
// 每段含 ti(小标题)与 txt(正文),作为兜底。
type ArtistDescResponse struct {
	Code         int    `json:"code"`
	BriefDesc    string `json:"briefDesc"`
	Introduction []struct {
		Ti  string `json:"ti"`
		Txt string `json:"txt"`
	} `json:"introduction"`
}

// SimiArtistResponse 映射 /simi/artist 的响应。
type SimiArtistResponse struct {
	Code    int      `json:"code"`
	Artists []Artist `json:"artists"`
}

// TopSongResponse 映射 /artist/top/song 的响应。
type TopSongResponse struct {
	Code  int       `json:"code"`
	Songs []SongHit `json:"songs"`
}

// ---- 歌曲(搜索结果与热门歌曲共用) ----

// SongHit 同时覆盖 /search?type=1 与 /artist/top/song 的歌曲条目。
// 搜索结果用 artists/album,热门歌曲用 ar/al;两套字段都解析,取非空者。
type SongHit struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Artists []struct {
		Name string `json:"name"`
	} `json:"artists"`
	Ar []struct {
		Name string `json:"name"`
	} `json:"ar"`
	Album struct {
		Name string `json:"name"`
	} `json:"album"`
	Al struct {
		Name string `json:"name"`
	} `json:"al"`
}

// firstArtist 返回首位歌手名,兼容 artists 与 ar 两种字段。
func (s SongHit) firstArtist() string {
	if len(s.Artists) > 0 {
		return s.Artists[0].Name
	}
	if len(s.Ar) > 0 {
		return s.Ar[0].Name
	}
	return ""
}

// albumName 返回专辑名,兼容 album 与 al 两种字段。
func (s SongHit) albumName() string {
	if s.Album.Name != "" {
		return s.Album.Name
	}
	return s.Al.Name
}

// ---- 专辑 ----

// AlbumBrief 是搜索结果里的专辑条目。
type AlbumBrief struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	PicURL string `json:"picUrl"`
	Artist struct {
		Name string `json:"name"`
	} `json:"artist"`
}

// AlbumDetailResponse 映射 /album?id= 的响应。
type AlbumDetailResponse struct {
	Code  int `json:"code"`
	Album struct {
		ID          int64  `json:"id"`
		Name        string `json:"name"`
		PicURL      string `json:"picUrl"`
		Description string `json:"description"`
		BriefDesc   string `json:"briefDesc"`
		Artist      struct {
			Name string `json:"name"`
		} `json:"artist"`
	} `json:"album"`
}

// ---- 歌词 ----

// LyricNewResponse 映射 /lyric/new 的响应。
// lrc 为标准 LRC,tlyric 为中文翻译,yrc 为逐字歌词(优先级最低,Navidrome 暂不解析逐字)。
type LyricNewResponse struct {
	Code   int       `json:"code"`
	Lrc    lyricPart `json:"lrc"`
	Tlyric lyricPart `json:"tlyric"`
}

type lyricPart struct {
	Lyric string `json:"lyric"`
}

// ---- 直连模式(官方 API) ----

// DirectArtistResponse 映射官方 /api/v1/artist/{id} 的响应。
// 官方 API 把歌手数据统一包在 artist 对象下,briefDesc 与 hotSongs 都在这一层。
type DirectArtistResponse struct {
	Code   int    `json:"code"`
	Artist struct {
		ID        int64  `json:"id"`
		Name      string `json:"name"`
		BriefDesc string `json:"briefDesc"`
		HotSongs  []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
			Ar   []struct {
				Name string `json:"name"`
			} `json:"ar"`
			Al struct {
				Name string `json:"name"`
			} `json:"al"`
		} `json:"hotSongs"`
	} `json:"artist"`
}

// ---- 相似歌曲 ----

// SimiSongResponse 映射 /song/similar 的响应。
// proxy 与 direct 模式共用此结构。
type SimiSongResponse struct {
	Code  int           `json:"code"`
	Songs []SimiSongItem `json:"songs"`
}

// SimiSongItem 是相似歌曲条目,覆盖网易云返回的歌曲字段。
type SimiSongItem struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Artists []struct {
		Name string `json:"name"`
	} `json:"artists"`
	Album struct {
		Name string `json:"name"`
	} `json:"album"`
	Duration int32 `json:"duration"`
}

// DirectAlbumResponse 映射官方 /api/v1/album/{id} 的响应。
type DirectAlbumResponse struct {
	Code  int `json:"code"`
	Album struct {
		ID          int64  `json:"id"`
		Name        string `json:"name"`
		PicURL      string `json:"picUrl"`
		Description string `json:"description"`
		BriefDesc   string `json:"briefDesc"`
		Artist      struct {
			Name string `json:"name"`
		} `json:"artist"`
	} `json:"album"`
}
