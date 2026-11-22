// Package style modela un estilo SLD con alcance de workspace.
package style

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

// ErrNotFound se devuelve cuando el estilo no existe.
var ErrNotFound = errors.New("style: no encontrado")

// Style es un estilo SLD publicado en un workspace.
type Style struct {
	Workspace string
	Name      string
	SLD       string // contenido XML completo del SLD
}

// Validate aplica las reglas mínimas de la entidad.
func (s Style) Validate() error {
	switch {
	case s.Workspace == "":
		return errors.New("style: el workspace es obligatorio")
	case s.Name == "":
		return errors.New("style: el nombre no puede estar vacío")
	case strings.TrimSpace(s.SLD) == "":
		return errors.New("style: el contenido SLD no puede estar vacío")
	case !strings.Contains(s.SLD, "StyledLayerDescriptor"):
		return errors.New("style: el contenido no parece un SLD (falta StyledLayerDescriptor)")
	}
	return nil
}

// SameSLD indica si dos SLD son equivalentes. GeoServer reserializa el SLD al
// subirlo (prefijos de espacio de nombres, saltos de línea, atributos
// xsi:schemaLocation y los <Name> de NamedLayer/UserStyle/FeatureTypeStyle),
// así que se comparan de forma semántica: estructura de elementos, atributos
// y texto, ignorando prefijos, espacios, comentarios y esos nombres. Si alguno
// no es XML válido se comparan como texto.
func SameSLD(a, b string) bool {
	ca, errA := canonical(a)
	cb, errB := canonical(b)
	if errA != nil || errB != nil {
		return strings.TrimSpace(strings.ReplaceAll(a, "\r\n", "\n")) == strings.TrimSpace(strings.ReplaceAll(b, "\r\n", "\n"))
	}
	return ca == cb
}

// Elementos cuyo hijo <Name> reescribe GeoServer al almacenar el estilo.
var renamedParents = map[string]bool{"NamedLayer": true, "UserStyle": true, "FeatureTypeStyle": true}

func canonical(doc string) (string, error) {
	dec := xml.NewDecoder(strings.NewReader(doc))
	var (
		out   strings.Builder
		stack []string
		skip  int // profundidad dentro de un <Name> ignorado
	)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if skip > 0 || (t.Name.Local == "Name" && len(stack) > 0 && renamedParents[stack[len(stack)-1]]) {
				skip++
				stack = append(stack, t.Name.Local)
				continue
			}
			var attrs []string
			for _, at := range t.Attr {
				if at.Name.Space == "xmlns" || at.Name.Local == "xmlns" || at.Name.Local == "schemaLocation" {
					continue
				}
				attrs = append(attrs, at.Name.Local+"="+at.Value)
			}
			sort.Strings(attrs)
			fmt.Fprintf(&out, "<%s %s>", t.Name.Local, strings.Join(attrs, " "))
			stack = append(stack, t.Name.Local)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
			if skip > 0 {
				skip--
				continue
			}
			out.WriteString("</>")
		case xml.CharData:
			if skip == 0 {
				if txt := strings.Join(strings.Fields(string(t)), " "); txt != "" {
					out.WriteString(txt)
				}
			}
		}
	}
	return out.String(), nil
}

// Repository abstrae la persistencia de Style (implementada en infrastructure).
type Repository interface {
	Get(ctx context.Context, workspace, name string) (*Style, error)
	List(ctx context.Context, workspace string) ([]string, error)
	Create(ctx context.Context, s Style) error
	Update(ctx context.Context, s Style) error
	Delete(ctx context.Context, workspace, name string) error
}

// Service contiene la lógica de negocio sobre Style.
type Service struct{ repo Repository }

func NewService(repo Repository) *Service { return &Service{repo: repo} }

// Get obtiene un estilo con su SLD.
func (s *Service) Get(ctx context.Context, workspace, name string) (*Style, error) {
	return s.repo.Get(ctx, workspace, name)
}

// List devuelve los nombres de los estilos de un workspace.
func (s *Service) List(ctx context.Context, workspace string) ([]string, error) {
	return s.repo.List(ctx, workspace)
}

// Ensure crea el estilo si no existe; si existe no hace nada (idempotente).
func (s *Service) Ensure(ctx context.Context, st Style) (*Style, error) {
	if err := st.Validate(); err != nil {
		return nil, fmt.Errorf("service: estilo inválido: %w", err)
	}
	existing, err := s.repo.Get(ctx, st.Workspace, st.Name)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("service: error consultando estilo %q: %w", st.Name, err)
	}
	if existing != nil {
		return existing, nil
	}
	if err := s.repo.Create(ctx, st); err != nil {
		return nil, fmt.Errorf("service: error creando estilo %q: %w", st.Name, err)
	}
	return s.repo.Get(ctx, st.Workspace, st.Name)
}

// Update reemplaza el SLD de un estilo existente.
func (s *Service) Update(ctx context.Context, st Style) error {
	if err := st.Validate(); err != nil {
		return fmt.Errorf("service: estilo inválido: %w", err)
	}
	if err := s.repo.Update(ctx, st); err != nil {
		return fmt.Errorf("service: error actualizando estilo %q: %w", st.Name, err)
	}
	return nil
}

// Remove borra el estilo.
func (s *Service) Remove(ctx context.Context, workspace, name string) error {
	if err := s.repo.Delete(ctx, workspace, name); err != nil {
		return fmt.Errorf("service: error eliminando estilo %q: %w", name, err)
	}
	return nil
}
