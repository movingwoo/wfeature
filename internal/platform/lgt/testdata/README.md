# LGT text-input fixture

`text-input.zip` is an authored AOT ARM archive for Host text-input tests. Its
initializer loads a minimal Java lightweight-component surface and leaves one
empty, focused, visible `TextFieldComponent` with an unrestricted 16-unit
limit. It does not implement rendering, keypad composition, input-method
listener callbacks, or persistence.

Regenerate the archive from this directory:

```sh
go run generate_text_input.go
```
