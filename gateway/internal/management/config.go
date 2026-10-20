package management

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"dnf90.local/gateway/internal/modules/dnf/adminapi"
	"dnf90.local/gateway/internal/servicehttp"
	"github.com/go-sql-driver/mysql"
)

// Config belongs to GM, not to game instance generation. Secrets stay in a
// protected config file. No field is echoed by the command or HTTP status.
type Config struct {
	GameRestartSocket   string              `json:"gameRestartSocket,omitempty"`
	Catalog             *servicehttp.Config `json:"catalog,omitempty"`
	ServiceToken        string              `json:"serviceToken,omitempty"`
	Listen              string              `json:"listen"`
	Password            string              `json:"password,omitempty"`
	LoginKeySHA256      string              `json:"loginKeySha256,omitempty"`
	WebRoot             string              `json:"webRoot"`
	SessionStorePath    string              `json:"sessionStorePath,omitempty"`
	RegistrationDSN     string              `json:"registrationDsn"`
	RepositoryDSN       string              `json:"repositoryDsn"`
	RegistrationProfile string              `json:"registrationProfile,omitempty"`
	AccountTable        string              `json:"accountTable"`
	Databases           []string            `json:"databases"`
	TablePrefix         string              `json:"tablePrefix"`
	PVFPath             string              `json:"pvfPath"`
	PVFSHA256           string              `json:"pvfSha256"`
	PVFIndexPath        string              `json:"pvfIndexPath,omitempty"`
	ItemIndexPath       string              `json:"itemIndexPath"`
	GameAPI             string              `json:"gameApi"`
	GameToken           string              `json:"gameToken"`
	ScanSeconds         int                 `json:"scanSeconds"`
}

func ReadConfig(path string) (Config, error) {
	var cfg Config
	f, err := os.Open(path)
	if err != nil {
		return cfg, errors.New("GM config cannot be opened")
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 65537))
	d.DisallowUnknownFields()
	if d.Decode(&cfg) != nil || d.Decode(new(any)) != io.EOF {
		return cfg, errors.New("invalid GM config JSON")
	}
	return cfg, cfg.Validate()
}

func (c Config) Validate() error {
	if c.GameRestartSocket != "" && (!filepath.IsAbs(c.GameRestartSocket) || filepath.Clean(c.GameRestartSocket) != c.GameRestartSocket) {
		return errors.New("game restart socket requires a clean absolute path")
	}
	if c.RegistrationProfile != "" && c.RegistrationProfile != "legacy-90cn" {
		return errors.New("unknown registration profile")
	}
	host, _, err := net.SplitHostPort(c.Listen)
	ip := net.ParseIP(host)
	if err != nil || ip == nil || (!ip.IsLoopback() && !ip.IsPrivate()) {
		return errors.New("GM listener must use an explicit private or loopback IP")
	}
	if c.LoginKeySHA256 != "" && (!regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(c.LoginKeySHA256) || c.Password != "") {
		return errors.New("configure exactly one valid management credential")
	}
	if c.LoginKeySHA256 == "" && len(c.Password) < 32 {
		return errors.New("GM password must have at least 32 characters")
	}
	for _, dsn := range []string{c.RegistrationDSN, c.RepositoryDSN} {
		parsed, err := mysql.ParseDSN(dsn)
		if err != nil || parsed.DBName == "" || !parsed.ParseTime || parsed.MultiStatements {
			return errors.New("GM requires explicit MariaDB databases, parseTime=true and no multiStatements")
		}
	}
	id := regexp.MustCompile(`^[A-Za-z0-9_]{1,40}$`)
	if !id.MatchString(c.AccountTable) || !id.MatchString(c.TablePrefix) || len(c.Databases) == 0 {
		return errors.New("invalid GM database mapping")
	}
	for _, db := range c.Databases {
		if !id.MatchString(db) {
			return errors.New("invalid GM database name")
		}
	}
	if (c.Catalog == nil && c.PVFPath == "") || !regexp.MustCompile(`^[a-fA-F0-9]{64}$`).MatchString(c.PVFSHA256) {
		return errors.New("GM requires an explicit PVF path and SHA256")
	}
	if c.Catalog == nil && c.ItemIndexPath == "" {
		return errors.New("GM requires an independent index path")
	}
	if _, err := adminapi.NewClient(c.GameAPI, c.GameToken); err != nil {
		return err
	}
	if c.ScanSeconds < 5 || c.ScanSeconds > 3600 {
		return errors.New("GM scan interval must be 5..3600 seconds")
	}
	if c.Catalog != nil {
		if c.Catalog.PVFSHA256 != c.PVFSHA256 || len(c.ServiceToken) < 32 {
			return errors.New("invalid external catalog identity")
		}
		if _, e := servicehttp.New(*c.Catalog); e != nil {
			return e
		}
	} else if c.ServiceToken != "" {
		return errors.New("service token requires external catalog")
	}
	return nil
}

func (c Config) scanInterval() time.Duration { return time.Duration(c.ScanSeconds) * time.Second }

func (c Config) DiskIndexPath() string {
	if c.PVFIndexPath != "" {
		return c.PVFIndexPath
	}
	return c.ItemIndexPath + ".pvfidx"
}
