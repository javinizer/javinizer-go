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
	UniqueID []uniqueID `xml:"uniqueid,omitempty"`

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
	Ratings ratings `xml:"ratings,omitempty"`

	// People
	Director string  `xml:"director,omitempty"`
	Actors   []actor `xml:"actor,omitempty"`
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
	Fanart  *fanart `xml:"fanart,omitempty"`
	Trailer string  `xml:"trailer,omitempty"`

	// File info (optional)
	FileInfo     *fileInfo `xml:"fileinfo,omitempty"`
	OriginalPath string    `xml:"originalpath,omitempty"` // Original source filename
}

// uniqueID represents a unique identifier with a type
type uniqueID struct {
	Type    string `xml:"type,attr"`
	Default bool   `xml:"default,attr,omitempty"`
	Value   string `xml:",chardata"`
}

// ratings contains rating information
type ratings struct {
	Rating []rating `xml:"rating,omitempty"`
}

// rating represents a single rating source
type rating struct {
	Name    string  `xml:"name,attr,omitempty"`
	Max     int     `xml:"max,attr,omitempty"`
	Default bool    `xml:"default,attr,omitempty"`
	Value   float64 `xml:"value"`
	Votes   int     `xml:"votes,omitempty"`
}

// actor represents an actress/actor in the movie
type actor struct {
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

// fanart contains fanart/background images
type fanart struct {
	Thumbs []Thumb `xml:"thumb,omitempty"`
}

// fileInfo contains media file technical information
type fileInfo struct {
	StreamDetails *streamDetails `xml:"streamdetails,omitempty"`
}

// streamDetails contains video/audio/subtitle stream information
type streamDetails struct {
	Video    []videoStream    `xml:"video,omitempty"`
	Audio    []audioStream    `xml:"audio,omitempty"`
	Subtitle []subtitleStream `xml:"subtitle,omitempty"`
}

// videoStream represents video stream information
type videoStream struct {
	Codec             string  `xml:"codec,omitempty"`
	Aspect            float64 `xml:"aspect,omitempty"`
	Width             int     `xml:"width,omitempty"`
	Height            int     `xml:"height,omitempty"`
	DurationInSeconds int     `xml:"durationinseconds,omitempty"`
	StereoMode        string  `xml:"stereomode,omitempty"`
}

// audioStream represents audio stream information
type audioStream struct {
	Codec    string `xml:"codec,omitempty"`
	Language string `xml:"language,omitempty"`
	Channels int    `xml:"channels,omitempty"`
}

// subtitleStream represents subtitle stream information
type subtitleStream struct {
	Language string `xml:"language,omitempty"`
}
