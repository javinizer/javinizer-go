package nfo

import (
	"encoding/xml"
)

// Movie represents a Kodi-compatible NFO movie structure
type Movie struct {
	XMLName xml.Name `xml:"movie"`

	// Basic identification
	Title         string `xml:"title,omitempty"`
	OriginalTitle string `xml:"originaltitle,omitempty"`
	SortTitle     string `xml:"sorttitle,omitempty"`

	// IDs
	ID       string     `xml:"id,omitempty"`
	UniqueID []UniqueID `xml:"uniqueid,omitempty"`

	// Plot/Description
	Plot    string `xml:"plot,omitempty"`
	Outline string `xml:"outline,omitempty"` // Short description
	Tagline string `xml:"tagline,omitempty"`

	// Time information
	Runtime     int    `xml:"runtime,omitempty"`     // in minutes
	Year        int    `xml:"year,omitempty"`        // Release year
	ReleaseDate string `xml:"releasedate,omitempty"` // YYYY-MM-DD format
	Premiered   string `xml:"premiered,omitempty"`   // YYYY-MM-DD format

	// Rating
	Ratings Ratings `xml:"ratings,omitempty"`

	// People
	Director string  `xml:"director,omitempty"`
	Actors   []Actor `xml:"actor,omitempty"`
	Credits  string  `xml:"credits,omitempty"` // Writer/credits

	// Production info
	Studio string `xml:"studio,omitempty"` // Production studio
	Maker  string `xml:"maker,omitempty"`  // Custom field for JAV maker
	Label  string `xml:"label,omitempty"`  // Custom field for JAV label
	Set    string `xml:"set,omitempty"`    // Series name

	// Categories
	Genres []string `xml:"genre,omitempty"`
	Tags   []string `xml:"tag,omitempty"`

	// Media
	Thumb   []Thumb `xml:"thumb,omitempty"`
	Fanart  *Fanart `xml:"fanart,omitempty"`
	Trailer string  `xml:"trailer,omitempty"`

	// File info (optional)
	FileInfo     *FileInfo `xml:"fileinfo,omitempty"`
	OriginalPath string    `xml:"originalpath,omitempty"` // Original source filename
}

// UniqueID represents a unique identifier with a type
type UniqueID struct {
	Type    string `xml:"type,attr"`
	Default bool   `xml:"default,attr,omitempty"`
	Value   string `xml:",chardata"`
}

// Ratings contains rating information
type Ratings struct {
	Rating []Rating `xml:"rating,omitempty"`
}

// Rating represents a single rating source
type Rating struct {
	Name    string  `xml:"name,attr,omitempty"`
	Max     int     `xml:"max,attr,omitempty"`
	Default bool    `xml:"default,attr,omitempty"`
	Value   float64 `xml:"value"`
	Votes   int     `xml:"votes,omitempty"`
}

// Actor represents an actress/actor in the movie
type Actor struct {
	Name    string `xml:"name"`
	AltName string `xml:"altname,omitempty"` // Alternative/romanized name
	Role    string `xml:"role,omitempty"`
	Order   int    `xml:"order,omitempty"`
	Thumb   string `xml:"thumb,omitempty"`
}

// Thumb represents a thumbnail/poster image
type Thumb struct {
	Aspect  string `xml:"aspect,attr,omitempty"`  // poster, banner, clearart, etc.
	Preview string `xml:"preview,attr,omitempty"` // Preview URL
	Value   string `xml:",chardata"`              // Main URL
}

// Fanart contains fanart/background images
type Fanart struct {
	Thumbs []Thumb `xml:"thumb,omitempty"`
}

// FileInfo contains media file technical information
type FileInfo struct {
	StreamDetails *StreamDetails `xml:"streamdetails,omitempty"`
}

// StreamDetails contains video/audio/subtitle stream information
type StreamDetails struct {
	Video    []VideoStream    `xml:"video,omitempty"`
	Audio    []AudioStream    `xml:"audio,omitempty"`
	Subtitle []SubtitleStream `xml:"subtitle,omitempty"`
}

// VideoStream represents video stream information
type VideoStream struct {
	Codec             string  `xml:"codec,omitempty"`
	Aspect            float64 `xml:"aspect,omitempty"`
	Width             int     `xml:"width,omitempty"`
	Height            int     `xml:"height,omitempty"`
	DurationInSeconds int     `xml:"durationinseconds,omitempty"`
	StereoMode        string  `xml:"stereomode,omitempty"`
}

// AudioStream represents audio stream information
type AudioStream struct {
	Codec    string `xml:"codec,omitempty"`
	Language string `xml:"language,omitempty"`
	Channels int    `xml:"channels,omitempty"`
}

// SubtitleStream represents subtitle stream information
type SubtitleStream struct {
	Language string `xml:"language,omitempty"`
}
