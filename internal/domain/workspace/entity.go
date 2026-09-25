package workspace

import "errors"

// Workspace es la entidad de dominio, independiente de GeoServer o Terraform.
type Workspace struct {
	Name     string
	Isolated bool
}

// Validate aplica las reglas de negocio mínimas de la entidad.
func (w Workspace) Validate() error {
	if w.Name == "" {
		return errors.New("workspace: el nombre no puede estar vacío")
	}
	return nil
}