package storage

import (
	"bytes"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/neocaos/geoform/internal/application"
)

var nonEnv = regexp.MustCompile(`[^A-Z0-9]+`)

// PasswordEnvName devuelve el nombre de variable de entorno usado como
// marcador de password para un datastore al exportar la configuración.
func PasswordEnvName(workspace, store string) string {
	n := strings.Trim(nonEnv.ReplaceAllString(strings.ToUpper(workspace+"_"+store), "_"), "_")
	return "GEOFORM_" + n + "_PASSWORD"
}

// Marshal serializa d al formato del fichero de configuración. Nunca escribe
// passwords: cada datastore lleva un password_env (ver PasswordEnvName).
func Marshal(d application.Desired) ([]byte, error) {
	var cfg fileConfig
	for _, w := range d.Workspaces {
		fw := fileWorkspace{Name: w.Name, Isolated: w.Isolated}
		for _, ds := range d.Datastores {
			if ds.Workspace != w.Name {
				continue
			}
			fd := fileDatastore{
				Name: ds.Name, Host: ds.PostGIS.Host, Port: ds.PostGIS.Port, Database: ds.PostGIS.Database,
				Schema: ds.PostGIS.Schema, User: ds.PostGIS.User, PasswordEnv: PasswordEnvName(w.Name, ds.Name),
			}
			for _, l := range d.Layers {
				if l.Workspace != w.Name || l.Store != ds.Name {
					continue
				}
				fl := fileLayer{Name: l.Name, Title: l.Title, SRS: l.SRS, Style: l.Style}
				if l.Table != l.Name {
					fl.Table = l.Table
				}
				fd.Layers = append(fd.Layers, fl)
			}
			fw.Datastores = append(fw.Datastores, fd)
		}
		// Los SLD se escriben inline (sld:); un SLD no lleva secretos.
		for _, st := range d.Styles {
			if st.Workspace == w.Name {
				fw.Styles = append(fw.Styles, fileStyle{Name: st.Name, SLD: st.SLD})
			}
		}
		for _, g := range d.LayerGroups {
			if g.Workspace == w.Name {
				fw.LayerGroups = append(fw.LayerGroups, fileLayerGroup{Name: g.Name, Title: g.Title, Mode: g.Mode, Layers: g.Layers})
			}
		}
		cfg.Workspaces = append(cfg.Workspaces, fw)
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(cfg); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
