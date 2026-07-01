// Netease Music plugin for Navidrome.
//
// 从自建 NeteaseCloudMusicApi 获取歌手与专辑元数据、歌词。
//
// Build:
//
//	tinygo build -o plugin.wasm -target wasip1 -buildmode=c-shared .
//
// Package:
//
//	zip -j netease.ndp manifest.json plugin.wasm
package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/navidrome/navidrome/plugins/pdk/go/lyrics"
	"github.com/navidrome/navidrome/plugins/pdk/go/metadata"
	"github.com/navidrome/navidrome/plugins/pdk/go/pdk"
)

const (
	defaultSearchLimit = 5
	// 网易图片尺寸仅作选择提示,非真实像素。
	picSize    = 1000
	img1v1Size = 500
	albumPicSz = 1000
	// 歌手头像下载缩放参数:网易云 CDN 支持 ?param=WxH 服务端缩放,
	// 头像展示尺寸较小,拉缩略图可显著降低体积、加快加载。
	artistImgParam = "param=200y200"
)

// withImgParam 给网易云图片 URL 追加 CDN 缩放参数。
func withImgParam(rawURL, param string) string {
	if rawURL == "" || param == "" {
		return rawURL
	}
	sep := "?"
	if strings.Contains(rawURL, "?") {
		sep = "&"
	}
	return rawURL + sep + param
}

// neteasePlugin 同时实现 MetadataAgent 与 Lyrics 两种 capability。
type neteasePlugin struct{}

func init() {
	p := &neteasePlugin{}
	metadata.Register(p)
	lyrics.Register(p)
}

func main() {}

// 编译期断言:确认实现了期望的 Provider 接口。
var (
	_ metadata.ArtistImagesProvider    = (*neteasePlugin)(nil)
	_ metadata.ArtistBiographyProvider = (*neteasePlugin)(nil)
	_ metadata.ArtistTopSongsProvider  = (*neteasePlugin)(nil)
	_ metadata.SimilarArtistsProvider  = (*neteasePlugin)(nil)
	_ metadata.AlbumInfoProvider       = (*neteasePlugin)(nil)
	_ metadata.AlbumImagesProvider     = (*neteasePlugin)(nil)
	_ lyrics.Lyrics                    = (*neteasePlugin)(nil)
)

// ---- 配置 ----

// newClientFromConfig 已移至 client.go,这里只保留行为调优相关配置读取。

func searchLimit() int {
	if v, ok := pdk.GetConfig("search_limit"); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			return n
		}
	}
	return defaultSearchLimit
}

func strictNameMatch() bool {
	return boolConfig("strict_name_match", true)
}

func translatedLyricsEnabled() bool {
	return boolConfig("enable_translated_lyrics", true)
}

func boolConfig(key string, def bool) bool {
	if v, ok := pdk.GetConfig(key); ok {
		if b, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
			return b
		}
	}
	return def
}

// ---- 通用查找 ----

// findArtist 搜索歌手并按严格名称匹配挑选(配置关闭时取第一条),避免给冷门艺人配错信息。
func (p *neteasePlugin) findArtist(c *client, name string) (*Artist, error) {
	artists, err := c.searchArtists(name, searchLimit())
	if err != nil {
		return nil, err
	}
	if !strictNameMatch() {
		return &artists[0], nil
	}
	for i := range artists {
		if strings.EqualFold(artists[i].Name, name) {
			return &artists[i], nil
		}
	}
	pdk.Log(pdk.LogDebug, fmt.Sprintf("netease: 无严格匹配 searched=%q top=%q", name, artists[0].Name))
	return nil, ErrNotFound
}

// findAlbum 搜索专辑并按 名称+歌手 双重匹配挑选。
func (p *neteasePlugin) findAlbum(c *client, albumName, artist string) (*AlbumBrief, error) {
	keywords := strings.TrimSpace(artist + " " + albumName)
	albums, err := c.searchAlbums(keywords, searchLimit())
	if err != nil {
		return nil, err
	}
	if !strictNameMatch() {
		return &albums[0], nil
	}
	for i := range albums {
		nameOK := strings.EqualFold(albums[i].Name, albumName)
		artistOK := artist == "" || strings.EqualFold(albums[i].Artist.Name, artist)
		if nameOK && artistOK {
			return &albums[i], nil
		}
	}
	return nil, ErrNotFound
}

// ---- MetadataAgent ----

func (p *neteasePlugin) GetArtistImages(input metadata.ArtistRequest) (*metadata.ArtistImagesResponse, error) {
	c, err := newClientFromConfig()
	if err != nil {
		return nil, err
	}
	artist, err := p.findArtist(c, input.Name)
	if err != nil {
		return nil, err
	}

	var images []metadata.ImageInfo
	for _, img := range []struct {
		URL  string
		Size int32
	}{
		{artist.PicURL, picSize},
		{artist.Img1v1URL, img1v1Size},
	} {
		if img.URL != "" {
			images = append(images, metadata.ImageInfo{URL: withImgParam(img.URL, artistImgParam), Size: img.Size})
		}
	}
	if len(images) == 0 {
		return nil, ErrNotFound
	}
	return &metadata.ArtistImagesResponse{Images: images}, nil
}

func (p *neteasePlugin) GetArtistBiography(input metadata.ArtistRequest) (*metadata.ArtistBiographyResponse, error) {
	c, err := newClientFromConfig()
	if err != nil {
		return nil, err
	}
	artist, err := p.findArtist(c, input.Name)
	if err != nil {
		return nil, err
	}
	desc, err := c.getArtistDesc(artist.ID)
	if err != nil {
		return nil, err
	}
	return &metadata.ArtistBiographyResponse{Biography: desc}, nil
}

func (p *neteasePlugin) GetSimilarArtists(input metadata.SimilarArtistsRequest) (*metadata.SimilarArtistsResponse, error) {
	c, err := newClientFromConfig()
	if err != nil {
		return nil, err
	}
	artist, err := p.findArtist(c, input.Name)
	if err != nil {
		return nil, err
	}
	similar, err := c.getSimilarArtists(artist.ID)
	if err != nil {
		return nil, err
	}

	limit := int(input.Limit)
	out := make([]metadata.ArtistRef, 0, len(similar))
	for i := range similar {
		if limit > 0 && len(out) >= limit {
			break
		}
		out = append(out, metadata.ArtistRef{Name: similar[i].Name})
	}
	return &metadata.SimilarArtistsResponse{Artists: out}, nil
}

func (p *neteasePlugin) GetArtistTopSongs(input metadata.TopSongsRequest) (*metadata.TopSongsResponse, error) {
	c, err := newClientFromConfig()
	if err != nil {
		return nil, err
	}
	artist, err := p.findArtist(c, input.Name)
	if err != nil {
		return nil, err
	}
	songs, err := c.getArtistTopSongs(artist.ID)
	if err != nil {
		return nil, err
	}

	count := int(input.Count)
	out := make([]metadata.SongRef, 0, len(songs))
	for i := range songs {
		if count > 0 && len(out) >= count {
			break
		}
		out = append(out, metadata.SongRef{
			Name:   songs[i].Name,
			Artist: songs[i].firstArtist(),
			Album:  songs[i].albumName(),
		})
	}
	return &metadata.TopSongsResponse{Songs: out}, nil
}

func (p *neteasePlugin) GetAlbumInfo(input metadata.AlbumRequest) (*metadata.AlbumInfoResponse, error) {
	c, err := newClientFromConfig()
	if err != nil {
		return nil, err
	}
	brief, err := p.findAlbum(c, input.Name, input.Artist)
	if err != nil {
		return nil, err
	}
	detail, err := c.getAlbum(brief.ID)
	if err != nil {
		return nil, err
	}
	desc := detail.Album.Description
	if desc == "" {
		desc = detail.Album.BriefDesc
	}
	return &metadata.AlbumInfoResponse{
		Name:        detail.Album.Name,
		Description: desc,
		URL:         fmt.Sprintf("https://music.163.com/#/album?id=%d", detail.Album.ID),
	}, nil
}

func (p *neteasePlugin) GetAlbumImages(input metadata.AlbumRequest) (*metadata.AlbumImagesResponse, error) {
	c, err := newClientFromConfig()
	if err != nil {
		return nil, err
	}
	brief, err := p.findAlbum(c, input.Name, input.Artist)
	if err != nil {
		return nil, err
	}
	picURL := brief.PicURL
	if picURL == "" {
		if detail, derr := c.getAlbum(brief.ID); derr == nil {
			picURL = detail.Album.PicURL
		}
	}
	if picURL == "" {
		return nil, ErrNotFound
	}
	return &metadata.AlbumImagesResponse{
		Images: []metadata.ImageInfo{{URL: picURL, Size: albumPicSz}},
	}, nil
}

// ---- Lyrics ----

func (p *neteasePlugin) GetLyrics(input lyrics.GetLyricsRequest) (lyrics.GetLyricsResponse, error) {
	var empty lyrics.GetLyricsResponse
	c, err := newClientFromConfig()
	if err != nil {
		return empty, err
	}

	t := input.Track
	keywords := strings.TrimSpace(t.Artist + " " + t.Title)
	songs, err := c.searchSongs(keywords, searchLimit())
	if err != nil {
		return empty, err
	}

	song := pickSong(songs, t.Title, t.Artist)
	if song == nil {
		return empty, ErrNotFound
	}

	lyric, err := c.getLyric(song.ID)
	if err != nil {
		return empty, err
	}

	out := []lyrics.LyricsText{{Text: normalizeLrc(lyric.Lrc.Lyric)}}
	if translatedLyricsEnabled() && lyric.Tlyric.Lyric != "" {
		out = append(out, lyrics.LyricsText{Lang: "zh", Text: normalizeLrc(lyric.Tlyric.Lyric)})
	}
	return lyrics.GetLyricsResponse{Lyrics: out}, nil
}

// pickSong 按 标题+歌手 精确匹配挑选,失败时回退首条。
func pickSong(songs []SongHit, title, artist string) *SongHit {
	for i := range songs {
		if !strings.EqualFold(songs[i].Name, title) {
			continue
		}
		if artist == "" || strings.Contains(strings.ToLower(artist), strings.ToLower(songs[i].firstArtist())) ||
			strings.Contains(strings.ToLower(songs[i].firstArtist()), strings.ToLower(artist)) {
			return &songs[i]
		}
	}
	if len(songs) > 0 {
		return &songs[0]
	}
	return nil
}
