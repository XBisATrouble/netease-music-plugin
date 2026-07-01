package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/navidrome/navidrome/plugins/pdk/go/host"
	"github.com/navidrome/navidrome/plugins/pdk/go/pdk"
)

// ErrNotFound 表示 NCM 没有返回有效结果。
var ErrNotFound = errors.New("netease: not found")

const httpTimeoutMs = 10000

// API 模式常量
const (
	modeProxy  = "proxy"
	modeDirect = "direct"
)

// 直连模式官方 API 端点
const (
	directSearchBase  = "https://music.163.com/api/search/get/web"
	directArtistURL   = "https://music.163.com/api/v1/artist/"
	directAlbumURL    = "https://music.163.com/api/v1/album/"
	directSimiArtist  = "https://music.163.com/api/discovery/simiArtist"
	directSimiSongURL = "https://music.163.com/api/discovery/simiSong"
	directLyricURL    = "https://interface3.music.163.com/api/song/lyric"
	directUserAgent   = "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_3) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15"
	directLyricCookie = "os=osx; osver=MacOS-14.3.1-arm; appver=2.0.3.131777"
)

// client 封装对网易云 API 的访问,所有请求经由 host.HTTPSend(WASM 沙箱)。
//
// 支持两种模式:
//   - proxy:  通过自建 NeteaseCloudMusicApi 调用,所有请求发往 baseURL
//   - direct: 直接调网易云官方公开 API,使用 cookie(MUSIC_U)做可选鉴权
type client struct {
	mode    string
	baseURL string // proxy 模式使用
	cookie  string // direct 模式 Cookie(MUSIC_U)
}

// newClientFromConfig 从 PDK 配置构造 client,根据 api_mode 决定走哪种模式。
func newClientFromConfig() (*client, error) {
	mode, _ := pdk.GetConfig("api_mode")
	if mode != modeProxy {
		mode = modeDirect
	}
	apiURL, _ := pdk.GetConfig("api_url")
	cookie, _ := pdk.GetConfig("netease_cookie")

	if mode == modeProxy && strings.TrimSpace(apiURL) == "" {
		return nil, errors.New("代理模式下 api_url 不能为空")
	}

	return &client{
		mode:    mode,
		baseURL: strings.TrimRight(strings.TrimSpace(apiURL), "/"),
		cookie:  strings.TrimSpace(cookie),
	}, nil
}

// directHeaders 构造直连模式请求头。
// extra 中的 Cookie 与 c.cookie 用 "; " 拼接(避免相互覆盖)。
func (c *client) directHeaders(extra map[string]string) map[string]string {
	headers := map[string]string{
		"User-Agent": directUserAgent,
	}
	for k, v := range extra {
		if strings.EqualFold(k, "Cookie") && c.cookie != "" {
			headers[k] = v + "; " + c.cookie
		} else {
			headers[k] = v
		}
	}
	if c.cookie != "" && headers["Cookie"] == "" {
		headers["Cookie"] = c.cookie
	}
	return headers
}

// makeRequest 发起 GET 请求并把 JSON 响应解码到 response(代理模式专用)。
func (c *client) makeRequest(path string, params url.Values, response any) error {
	reqURL := c.baseURL + path
	if len(params) > 0 {
		reqURL += "?" + params.Encode()
	}

	resp, err := host.HTTPSend(host.HTTPRequest{
		Method:    "GET",
		URL:       reqURL,
		Headers:   map[string]string{"Accept": "application/json"},
		TimeoutMs: httpTimeoutMs,
	})
	if err != nil {
		return fmt.Errorf("netease request failed: %w", err)
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("netease error: HTTP %d", resp.StatusCode)
	}
	return json.Unmarshal(resp.Body, response)
}

// searchArtists 调用歌手搜索(type=100)。
func (c *client) searchArtists(name string, limit int) ([]Artist, error) {
	var out []Artist
	err := c.cached("s:ar:"+strconv.Itoa(limit)+":"+normKey(name), &out, func() error {
		artists, ferr := c.searchArtistsRaw(name, limit)
		out = artists
		return ferr
	})
	return out, err
}

func (c *client) searchArtistsRaw(name string, limit int) ([]Artist, error) {
	if c.mode == modeDirect {
		return c.searchDirect(name, 100, limit)
	}

	params := url.Values{}
	params.Set("keywords", name)
	params.Set("type", "100")
	params.Set("limit", strconv.Itoa(limit))

	var res SearchArtistResponse
	if err := c.makeRequest("/search", params, &res); err != nil {
		return nil, err
	}
	if res.Code != 200 || len(res.Result.Artists) == 0 {
		return nil, ErrNotFound
	}
	return res.Result.Artists, nil
}

// getArtistDesc 获取歌手简介。
func (c *client) getArtistDesc(artistID int64) (string, error) {
	var out string
	err := c.cached("a:desc:"+strconv.FormatInt(artistID, 10), &out, func() error {
		desc, ferr := c.getArtistDescRaw(artistID)
		out = desc
		return ferr
	})
	return out, err
}

func (c *client) getArtistDescRaw(artistID int64) (string, error) {
	if c.mode == modeDirect {
		return c.getArtistDescDirect(artistID)
	}

	params := url.Values{}
	params.Set("id", strconv.FormatInt(artistID, 10))

	var res ArtistDescResponse
	if err := c.makeRequest("/artist/desc", params, &res); err != nil {
		return "", err
	}
	if res.Code != 200 {
		return "", ErrNotFound
	}
	// briefDesc 优先(一段式综述,最像简介);为空时回退拼接 introduction
	// 各段(小标题\n正文,段间空行),覆盖只有分段资料的少数歌手。
	if res.BriefDesc != "" {
		return res.BriefDesc, nil
	}
	var segs []string
	for _, intro := range res.Introduction {
		switch {
		case intro.Ti != "" && intro.Txt != "":
			segs = append(segs, intro.Ti+"\n"+intro.Txt)
		case intro.Txt != "":
			segs = append(segs, intro.Txt)
		}
	}
	desc := strings.Join(segs, "\n\n")
	if desc == "" {
		return "", ErrNotFound
	}
	return desc, nil
}

// getSimilarArtists 获取相似歌手。
func (c *client) getSimilarArtists(artistID int64) ([]Artist, error) {
	var out []Artist
	err := c.cached("a:simi:"+strconv.FormatInt(artistID, 10), &out, func() error {
		artists, ferr := c.getSimilarArtistsRaw(artistID)
		out = artists
		return ferr
	})
	return out, err
}

func (c *client) getSimilarArtistsRaw(artistID int64) ([]Artist, error) {
	if c.mode == modeDirect {
		return c.getSimilarArtistsDirect(artistID)
	}

	params := url.Values{}
	params.Set("id", strconv.FormatInt(artistID, 10))

	var res SimiArtistResponse
	if err := c.makeRequest("/simi/artist", params, &res); err != nil {
		return nil, err
	}
	if len(res.Artists) == 0 {
		return nil, ErrNotFound
	}
	return res.Artists, nil
}

// getArtistTopSongs 获取歌手热门歌曲。
func (c *client) getArtistTopSongs(artistID int64) ([]SongHit, error) {
	var out []SongHit
	err := c.cached("a:top:"+strconv.FormatInt(artistID, 10), &out, func() error {
		songs, ferr := c.getArtistTopSongsRaw(artistID)
		out = songs
		return ferr
	})
	return out, err
}

func (c *client) getArtistTopSongsRaw(artistID int64) ([]SongHit, error) {
	if c.mode == modeDirect {
		return c.getArtistTopSongsDirect(artistID)
	}

	params := url.Values{}
	params.Set("id", strconv.FormatInt(artistID, 10))

	var res TopSongResponse
	if err := c.makeRequest("/artist/top/song", params, &res); err != nil {
		return nil, err
	}
	if len(res.Songs) == 0 {
		return nil, ErrNotFound
	}
	return res.Songs, nil
}

// searchAlbums 调用专辑搜索(type=10)。
func (c *client) searchAlbums(keywords string, limit int) ([]AlbumBrief, error) {
	var out []AlbumBrief
	err := c.cached("s:al:"+strconv.Itoa(limit)+":"+normKey(keywords), &out, func() error {
		albums, ferr := c.searchAlbumsRaw(keywords, limit)
		out = albums
		return ferr
	})
	return out, err
}

func (c *client) searchAlbumsRaw(keywords string, limit int) ([]AlbumBrief, error) {
	if c.mode == modeDirect {
		return c.searchDirectAlbums(keywords, limit)
	}

	params := url.Values{}
	params.Set("keywords", keywords)
	params.Set("type", "10")
	params.Set("limit", strconv.Itoa(limit))

	var res SearchAlbumResponse
	if err := c.makeRequest("/search", params, &res); err != nil {
		return nil, err
	}
	if res.Code != 200 || len(res.Result.Albums) == 0 {
		return nil, ErrNotFound
	}
	return res.Result.Albums, nil
}

// getAlbum 获取专辑详情(含描述)。
func (c *client) getAlbum(albumID int64) (*AlbumDetailResponse, error) {
	var out AlbumDetailResponse
	err := c.cached("al:"+strconv.FormatInt(albumID, 10), &out, func() error {
		detail, ferr := c.getAlbumRaw(albumID)
		if ferr == nil && detail != nil {
			out = *detail
		}
		return ferr
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *client) getAlbumRaw(albumID int64) (*AlbumDetailResponse, error) {
	if c.mode == modeDirect {
		return c.getAlbumDirect(albumID)
	}

	params := url.Values{}
	params.Set("id", strconv.FormatInt(albumID, 10))

	var res AlbumDetailResponse
	if err := c.makeRequest("/album", params, &res); err != nil {
		return nil, err
	}
	if res.Code != 200 || res.Album.ID == 0 {
		return nil, ErrNotFound
	}
	return &res, nil
}

// searchSongs 调用单曲搜索(type=1)。
func (c *client) searchSongs(keywords string, limit int) ([]SongHit, error) {
	var out []SongHit
	err := c.cached("s:so:"+strconv.Itoa(limit)+":"+normKey(keywords), &out, func() error {
		songs, ferr := c.searchSongsRaw(keywords, limit)
		out = songs
		return ferr
	})
	return out, err
}

func (c *client) searchSongsRaw(keywords string, limit int) ([]SongHit, error) {
	if c.mode == modeDirect {
		return c.searchDirectSongs(keywords, limit)
	}

	params := url.Values{}
	params.Set("keywords", keywords)
	params.Set("type", "1")
	params.Set("limit", strconv.Itoa(limit))

	var res SearchSongResponse
	if err := c.makeRequest("/search", params, &res); err != nil {
		return nil, err
	}
	if res.Code != 200 || len(res.Result.Songs) == 0 {
		return nil, ErrNotFound
	}
	return res.Result.Songs, nil
}

// getSimilarSongs 获取相似歌曲。
func (c *client) getSimilarSongs(songID int64) ([]SimiSongItem, error) {
	var out []SimiSongItem
	err := c.cached("s:simi:"+strconv.FormatInt(songID, 10), &out, func() error {
		songs, ferr := c.getSimilarSongsRaw(songID)
		out = songs
		return ferr
	})
	return out, err
}

func (c *client) getSimilarSongsRaw(songID int64) ([]SimiSongItem, error) {
	if c.mode == modeDirect {
		return c.getSimilarSongsDirect(songID)
	}

	params := url.Values{}
	params.Set("id", strconv.FormatInt(songID, 10))

	var res SimiSongResponse
	if err := c.makeRequest("/song/similar", params, &res); err != nil {
		return nil, err
	}
	if res.Code != 200 || len(res.Songs) == 0 {
		return nil, ErrNotFound
	}
	return res.Songs, nil
}

// getSimilarSongsDirect 直连模式获取相似歌曲。
// POST form-urlencoded,需要 cookie 才能获取有效结果。
func (c *client) getSimilarSongsDirect(songID int64) ([]SimiSongItem, error) {
	body := "songid=" + strconv.FormatInt(songID, 10)
	if c.cookie == "" {
		return nil, errors.New("获取相似歌曲需要填写 netease_cookie")
	}

	resp, err := host.HTTPSend(host.HTTPRequest{
		Method: "POST",
		URL:    directSimiSongURL,
		Headers: c.directHeaders(map[string]string{
			"Referer":      "https://music.163.com/",
			"Content-Type": "application/x-www-form-urlencoded",
		}),
		Body:      []byte(body),
		TimeoutMs: httpTimeoutMs,
	})
	if err != nil {
		return nil, fmt.Errorf("netease request failed: %w", err)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("netease error: HTTP %d", resp.StatusCode)
	}

	var res SimiSongResponse
	if err := json.Unmarshal(resp.Body, &res); err != nil {
		return nil, err
	}
	if len(res.Songs) == 0 {
		return nil, ErrNotFound
	}
	return res.Songs, nil
}

// getLyric 获取歌词(含翻译)。
func (c *client) getLyric(songID int64) (*LyricNewResponse, error) {
	var out LyricNewResponse
	err := c.cached("ly:"+strconv.FormatInt(songID, 10), &out, func() error {
		lyric, ferr := c.getLyricRaw(songID)
		if ferr == nil && lyric != nil {
			out = *lyric
		}
		return ferr
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *client) getLyricRaw(songID int64) (*LyricNewResponse, error) {
	if c.mode == modeDirect {
		return c.getLyricDirect(songID)
	}

	params := url.Values{}
	params.Set("id", strconv.FormatInt(songID, 10))

	var res LyricNewResponse
	if err := c.makeRequest("/lyric/new", params, &res); err != nil {
		return nil, err
	}
	if res.Lrc.Lyric == "" {
		return nil, ErrNotFound
	}
	return &res, nil
}

// ---- 直连模式实现 ----

// searchDirect 直连模式搜索。type 100=歌手,10=专辑,1=单曲。
// 返回 SearchArtistResponse,因为官方 API 的结构与代理一致,避免重复定义。
func (c *client) searchDirect(keywords string, searchType, limit int) ([]Artist, error) {
	u, err := url.Parse(directSearchBase)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("s", keywords)
	q.Set("type", strconv.Itoa(searchType))
	q.Set("offset", "0")
	q.Set("limit", strconv.Itoa(limit))
	u.RawQuery = q.Encode()

	resp, err := host.HTTPSend(host.HTTPRequest{
		Method:    "GET",
		URL:       u.String(),
		Headers:   c.directHeaders(map[string]string{"Referer": "https://music.163.com/"}),
		TimeoutMs: httpTimeoutMs,
	})
	if err != nil {
		return nil, fmt.Errorf("netease request failed: %w", err)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("netease error: HTTP %d", resp.StatusCode)
	}

	var res SearchArtistResponse
	if err := json.Unmarshal(resp.Body, &res); err != nil {
		return nil, err
	}
	if res.Code != 200 || len(res.Result.Artists) == 0 {
		return nil, ErrNotFound
	}
	return res.Result.Artists, nil
}

// searchDirectAlbums 直连模式专辑搜索。
func (c *client) searchDirectAlbums(keywords string, limit int) ([]AlbumBrief, error) {
	u, err := url.Parse(directSearchBase)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("s", keywords)
	q.Set("type", "10")
	q.Set("offset", "0")
	q.Set("limit", strconv.Itoa(limit))
	u.RawQuery = q.Encode()

	resp, err := host.HTTPSend(host.HTTPRequest{
		Method:    "GET",
		URL:       u.String(),
		Headers:   c.directHeaders(map[string]string{"Referer": "https://music.163.com/"}),
		TimeoutMs: httpTimeoutMs,
	})
	if err != nil {
		return nil, fmt.Errorf("netease request failed: %w", err)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("netease error: HTTP %d", resp.StatusCode)
	}

	var res SearchAlbumResponse
	if err := json.Unmarshal(resp.Body, &res); err != nil {
		return nil, err
	}
	if res.Code != 200 || len(res.Result.Albums) == 0 {
		return nil, ErrNotFound
	}
	return res.Result.Albums, nil
}

// searchDirectSongs 直连模式单曲搜索。
func (c *client) searchDirectSongs(keywords string, limit int) ([]SongHit, error) {
	u, err := url.Parse(directSearchBase)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("s", keywords)
	q.Set("type", "1")
	q.Set("offset", "0")
	q.Set("limit", strconv.Itoa(limit))
	u.RawQuery = q.Encode()

	resp, err := host.HTTPSend(host.HTTPRequest{
		Method:    "GET",
		URL:       u.String(),
		Headers:   c.directHeaders(map[string]string{"Referer": "https://music.163.com/"}),
		TimeoutMs: httpTimeoutMs,
	})
	if err != nil {
		return nil, fmt.Errorf("netease request failed: %w", err)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("netease error: HTTP %d", resp.StatusCode)
	}

	var res SearchSongResponse
	if err := json.Unmarshal(resp.Body, &res); err != nil {
		return nil, err
	}
	if res.Code != 200 || len(res.Result.Songs) == 0 {
		return nil, ErrNotFound
	}
	return res.Result.Songs, nil
}

// getArtistDescDirect 直连模式获取歌手简介。
// 官方 /api/v1/artist/{id} 在 artist 对象里返回 briefDesc。
func (c *client) getArtistDescDirect(artistID int64) (string, error) {
	resp, err := host.HTTPSend(host.HTTPRequest{
		Method:    "GET",
		URL:       directArtistURL + strconv.FormatInt(artistID, 10),
		Headers:   c.directHeaders(nil),
		TimeoutMs: httpTimeoutMs,
	})
	if err != nil {
		return "", fmt.Errorf("netease request failed: %w", err)
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("netease error: HTTP %d", resp.StatusCode)
	}

	var res DirectArtistResponse
	if err := json.Unmarshal(resp.Body, &res); err != nil {
		return "", err
	}
	if res.Code != 200 {
		return "", ErrNotFound
	}
	if res.Artist.BriefDesc == "" {
		return "", ErrNotFound
	}
	return res.Artist.BriefDesc, nil
}

// getSimilarArtistsDirect 直连模式获取相似歌手。
// POST form-urlencoded,需要 cookie 才能获取非空结果。
func (c *client) getSimilarArtistsDirect(artistID int64) ([]Artist, error) {
	body := "artistid=" + strconv.FormatInt(artistID, 10)
	// 如果还没设 cookie,提示用户;避免静默失败。
	if c.cookie == "" {
		return nil, errors.New("获取相似歌手需要填写 netease_cookie")
	}

	resp, err := host.HTTPSend(host.HTTPRequest{
		Method: "POST",
		URL:    directSimiArtist,
		Headers: c.directHeaders(map[string]string{
			"Referer":      "https://music.163.com/",
			"Content-Type": "application/x-www-form-urlencoded",
		}),
		Body:      []byte(body),
		TimeoutMs: httpTimeoutMs,
	})
	if err != nil {
		return nil, fmt.Errorf("netease request failed: %w", err)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("netease error: HTTP %d", resp.StatusCode)
	}

	var res SimiArtistResponse
	if err := json.Unmarshal(resp.Body, &res); err != nil {
		return nil, err
	}
	if len(res.Artists) == 0 {
		return nil, ErrNotFound
	}
	return res.Artists, nil
}

// getArtistTopSongsDirect 直连模式获取歌手热门歌曲。
// 官方 /api/v1/artist/{id} 在 artist.hotSongs 里返回热门歌曲。
func (c *client) getArtistTopSongsDirect(artistID int64) ([]SongHit, error) {
	resp, err := host.HTTPSend(host.HTTPRequest{
		Method:    "GET",
		URL:       directArtistURL + strconv.FormatInt(artistID, 10),
		Headers:   c.directHeaders(nil),
		TimeoutMs: httpTimeoutMs,
	})
	if err != nil {
		return nil, fmt.Errorf("netease request failed: %w", err)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("netease error: HTTP %d", resp.StatusCode)
	}

	var res DirectArtistResponse
	if err := json.Unmarshal(resp.Body, &res); err != nil {
		return nil, err
	}
	if res.Code != 200 || len(res.Artist.HotSongs) == 0 {
		return nil, ErrNotFound
	}

	// 官方 hotSongs 字段用 ar/al(短名),而 SongHit 同时解析了 ar 和 artists;
	// 我们的 SongHit 也支持 al,所以 JSON 反序列化已自动填充。
	songs := make([]SongHit, len(res.Artist.HotSongs))
	for i, hs := range res.Artist.HotSongs {
		songs[i] = SongHit{
			ID:   hs.ID,
			Name: hs.Name,
			Ar:   hs.Ar,
			Al:   hs.Al,
		}
	}
	return songs, nil
}

// getAlbumDirect 直连模式获取专辑详情。
func (c *client) getAlbumDirect(albumID int64) (*AlbumDetailResponse, error) {
	resp, err := host.HTTPSend(host.HTTPRequest{
		Method:    "GET",
		URL:       directAlbumURL + strconv.FormatInt(albumID, 10),
		Headers:   c.directHeaders(nil),
		TimeoutMs: httpTimeoutMs,
	})
	if err != nil {
		return nil, fmt.Errorf("netease request failed: %w", err)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("netease error: HTTP %d", resp.StatusCode)
	}

	var direct DirectAlbumResponse
	if err := json.Unmarshal(resp.Body, &direct); err != nil {
		return nil, err
	}
	if direct.Code != 200 || direct.Album.ID == 0 {
		return nil, ErrNotFound
	}
	// 转换为与代理模式同构的 AlbumDetailResponse,后续调用方逻辑可复用。
	res := &AlbumDetailResponse{Code: direct.Code}
	res.Album.ID = direct.Album.ID
	res.Album.Name = direct.Album.Name
	res.Album.PicURL = direct.Album.PicURL
	res.Album.Description = direct.Album.Description
	res.Album.BriefDesc = direct.Album.BriefDesc
	res.Album.Artist.Name = direct.Album.Artist.Name
	return res, nil
}

// getLyricDirect 直连模式获取歌词。
// POST form-urlencoded,需要 cookie 才能获取带翻译的歌词。
// 官方歌词端点需要 os/osver/appver cookie 才能返回数据。
func (c *client) getLyricDirect(songID int64) (*LyricNewResponse, error) {
	body := fmt.Sprintf("id=%d&cp=false&tv=0&lv=0&rv=0&kv=0&yv=0&ytv=0&yrv=0", songID)

	resp, err := host.HTTPSend(host.HTTPRequest{
		Method: "POST",
		URL:    directLyricURL,
		Headers: c.directHeaders(map[string]string{
			"Referer":      "https://music.163.com/",
			"Content-Type": "application/x-www-form-urlencoded",
			"Cookie":       directLyricCookie,
		}),
		Body:      []byte(body),
		TimeoutMs: httpTimeoutMs,
	})
	if err != nil {
		return nil, fmt.Errorf("netease request failed: %w", err)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("netease error: HTTP %d", resp.StatusCode)
	}

	var res LyricNewResponse
	if err := json.Unmarshal(resp.Body, &res); err != nil {
		return nil, err
	}
	if res.Lrc.Lyric == "" {
		return nil, ErrNotFound
	}
	return &res, nil
}

// ---- 预留扩展点:评论 ----
// Navidrome 当前没有承载"评论"的元数据字段或 UI 位置,故本插件暂不实现。
// NCM 端点 /comment/music?id= 可用,未来若 Navidrome 增加承载位,可在此补 getComments。
