# LGT text-input fixture

`text-input.zip` is an authored AOT ARM archive for Host text-input tests in
`internal/session` and `internal/webhost`. It holds an `app_info` (AID
`54585449`, PID `PF000001`, main class `TextInputFixture`) and `54585449.jar`,
whose `binary.mod` is the ELF module. Its initializer loads a minimal Java
lightweight-component surface and leaves one empty, focused, visible
`TextFieldComponent` in a shown `ShellComponent`, unrestricted and limited to
16 units. It does not implement rendering, keypad composition,
input-method listener callbacks, or persistence.

Regenerate the archive from this directory (the generator is excluded from
builds by `//go:build ignore`):

```sh
go run generate_text_input.go
```
