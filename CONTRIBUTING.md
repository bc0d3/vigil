# Contribuir a Vigil

¡Gracias por el interés! Vigil es deliberadamente chico: una responsabilidad,
sin dependencias externas. Mantengamos eso.

## Antes de mandar un PR

```bash
make check   # gofmt + go vet + go test -race
make lint    # golangci-lint (opcional pero recomendado)
```

Todo tiene que quedar verde.

## Principios de diseño (no negociables)

1. **Solo stdlib.** No agregar dependencias a `go.mod`.
2. **Salida = 1 línea JSON por recurso**, determinista, en snake_case.
3. **Se hashea el contenido crudo**, nunca normalizado.
4. **Un status HTTP no es un error.** Solo un fallo de red llena `error`.

Si tu cambio toca el contrato de salida, abrí primero un issue para discutirlo:
hay consumidores automatizados que dependen de su estabilidad.

## Estilo

- `gofmt` / `goimports` obligatorio.
- Commits en estilo [Conventional Commits](https://www.conventionalcommits.org/)
  (`feat:`, `fix:`, `docs:`, `test:`, `chore:`…). El changelog del release se
  arma a partir de ellos.

## Tests

Todo cambio de comportamiento necesita un test con `httptest`. Mirá
`main_test.go` como referencia.
