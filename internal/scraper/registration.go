package scraper

import (
	"github.com/javinizer/javinizer-go/internal/scraper/aventertainment"
	"github.com/javinizer/javinizer-go/internal/scraper/caribbeancom"
	"github.com/javinizer/javinizer-go/internal/scraper/dlgetchu"
	"github.com/javinizer/javinizer-go/internal/scraper/dmm"
	"github.com/javinizer/javinizer-go/internal/scraper/fc2"
	"github.com/javinizer/javinizer-go/internal/scraper/jav321"
	"github.com/javinizer/javinizer-go/internal/scraper/javbus"
	"github.com/javinizer/javinizer-go/internal/scraper/javdb"
	"github.com/javinizer/javinizer-go/internal/scraper/javlibrary"
	"github.com/javinizer/javinizer-go/internal/scraper/javstash"
	"github.com/javinizer/javinizer-go/internal/scraper/libredmm"
	"github.com/javinizer/javinizer-go/internal/scraper/mgstage"
	"github.com/javinizer/javinizer-go/internal/scraper/r18dev"
	"github.com/javinizer/javinizer-go/internal/scraper/tokyohot"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

func RegisterAll(reg scraperutil.ScraperRegistrar) {
	r18dev.Register(reg)
	dmm.Register(reg)
	javlibrary.Register(reg)
	javdb.Register(reg)
	javbus.Register(reg)
	mgstage.Register(reg)
	fc2.Register(reg)
	jav321.Register(reg)
	javstash.Register(reg)
	aventertainment.Register(reg)
	caribbeancom.Register(reg)
	dlgetchu.Register(reg)
	libredmm.Register(reg)
	tokyohot.Register(reg)
}
