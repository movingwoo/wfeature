# SGS service opcode inventory

This is an implementation and investigation map, not a claim of complete SGS support. It records the working tree on 2026-09-11. Tests and real acceptance paths must establish each implemented contract separately. Newly implemented services should update their rows.

The consulted original PC runtime uses opcode table `0x494a68`, with 243 entries (`0x00..0xf2`). `0xff` ends an invocation; `0xf3..0xfe` are invalid. The preceding three twelve-entry tables contain vector arithmetic helpers, not additional opcodes. Native virtual addresses identify evidence locations, not code to embed. No original code, font, image, or palette table is reproduced here.

Arguments are listed in guest call order, oldest operand first, where established. Values are signed 16-bit unless stated. Resource IDs select byte banks; word addresses select the mutable or constant integer banks. An unresolved signature must fail explicitly until evidence and bounds checks support implementation.

Status: **I** = a checked host dispatch path exists; **P** = a path exists with a material compatibility limitation or integration still pending; **M** = no host path yet; **R/M** = the original handler is reserved/empty but the host does not yet accept it. I does not assert byte-perfect compatibility or test coverage. Communication/UI operations are kept distinct from reserved no-ops.

| Opcode | Original handler | Status | Contract and remaining evidence |
|---|---|---|---|
| `0x51` | `0x00418980` | P | Device information: pop word address, write LCD class, color class, color count and audio class (four words). |
| `0x52` | `0x00418a50` | P | Copy device MIN into a resource and pop its ID. Original reads configured `NV_ROM`/`MIN`; current host supplies an empty string. See [host state](sgs-host-state.md). |
| `0x53` | `0x00418ad0` | M | Copy current script metadata `UserID` into a resource; replace its ID with role byte (0 outgoing, 1 incoming; standalone initializes 1). See [host state](sgs-host-state.md). |
| `0x54` | `0x00418b70` | I | Pop word address; clear five words through original helper. |
| `0x55` | `0x0040eb80` | I | Fill drawing buffer with raw byte 255. |
| `0x56` | `0x0040eba0` | I | Fill drawing buffer with raw byte 0. |
| `0x57` | `0x00418ba0` | I | Fill with mapped script color (pop one signed color modulo 182). |
| `0x58` | `0x00418bd0` | I | Plot (x,y,color); color 4 transparent, coordinates clipped. |
| `0x59` | `0x00418c20` | I | Select palette bank, clamped 0..6. Affects future drawing. |
| `0x5a` | `0x00418c60` | I | Push color constant 0. |
| `0x5b` | `0x00418c90` | I | Push color constant 3. |
| `0x5c` | `0x00418cc0` | I | Push color constant 4 (transparent). |
| `0x5d` | `0x00418cf0` | I | Push color constant 5. |
| `0x5e` | `0x00418d20` | I | Select drawing color from unsigned low byte modulo 182. |
| `0x5f` | `0x00418d50` | I | Draw line (x1,y1,x2,y2), inclusive endpoints. |
| `0x60` | `0x00418d90` | I | Draw horizontal line (x1,x2,y), inclusive endpoints. |
| `0x61` | `0x00418dd0` | I | Draw vertical line (x,y1,y2), inclusive endpoints. |
| `0x62` | `0x00418e10` | I | Outline rectangle (x1,y1,x2,y2), inclusive endpoints. |
| `0x63` | `0x00418e50` | I | Fill rectangle (x1,y1,x2,y2), inclusive endpoints. |
| `0x64` | `0x00418e90` | I | Outline ellipse through original primitive 0x40dfc0; four coordinate arguments. |
| `0x65` | `0x00418ed0` | I | Fill ellipse through original primitive 0x40e2f0; four coordinate arguments. |
| `0x66` | `0x00418f10` | I | Set text (style,foreground,background,alignment), reduced modulo 4/182/182/3. |
| `0x67` | `0x00418f80` | I | Set text style, unsigned low byte modulo 4. |
| `0x68` | `0x00418fb0` | I | Set text foreground/background, unsigned low bytes modulo 182. |
| `0x69` | `0x00418ff0` | I | Set text alignment, unsigned low byte modulo 3. |
| `0x6a` | `0x00419020` | P | Text (x,y,resource), selected style, transparent background. |
| `0x6b` | `0x004190e0` | P | Text (x,y,resource), selected style, filled background. |
| `0x6c` | `0x004191a0` | P | Text (x,y,resource), forced style 2, transparent background. |
| `0x6d` | `0x00419250` | P | Text (x,y,resource), forced style 2, filled background. |
| `0x6e` | `0x00419310` | I | Pack a palette from word-addressed source to word-addressed byte storage; two addresses, source begins with palette type. |
| `0x6f` | `0x00419420` | I | Bitmap (x,y,resource), embedded palette and signed origins. |
| `0x70` | `0x004194b0` | I | Bitmap (x,y,resource,mirror); nonzero mirror reverses x and changes origin calculation. |
| `0x71` | `0x00419550` | I | Bitmap (x,y,resource,paletteAddress), palette override from packed bytes. |
| `0x72` | `0x00419600` | I | Bitmap (x,y,resource,mirror,paletteAddress), mirrored palette override. |
| `0x73` | `0x0040d950` | I | Clear the original twenty-entry queued sprite list. No arguments. |
| `0x74` | `0x004196c0` | I | Five physical slots: reserved result, x, y, resource, mirror. Consumes all five and leaves success 0/1 in the reserved backing slot; stack-save/restore regression verifies its recovery. |
| `0x75` | `0x00419760` | I | Sort and draw queued sprites, pop sort mode 0..3 (ascending/descending x/y). Modes outside range preserve queue order. |
| `0x76` | `0x004197a0` | I | Copy drawing buffer into backup. |
| `0x77` | `0x004197b0` | I | Copy backup into drawing buffer. |
| `0x78` | `0x00419780` | I | Present drawing buffer; original overlay state suppresses ordinary presentation. |
| `0x79` | `0x004198c0` | I | Replace resource ID with allocated resource size in bytes. |
| `0x7a` | `0x00419910` | I | Resize (resource,size), return 0/1; original allocator preserves capacity on shrinking. |
| `0x7b` | `0x00419950` | I | Resize and zero resource prefix (resource,size). |
| `0x7c` | `0x004199c0` | I | Replace resource ID with NUL-terminated byte length. |
| `0x7d` | `0x00419a10` | I | Copy source string into destination resource (destination,source). |
| `0x7e` | `0x00419ac0` | I | Substring (destination,source,offset,length); bounded string copy then explicit NUL. |
| `0x7f` | `0x00419b70` | I | Append source string to destination resource. |
| `0x80` | `0x00419c60` | I | Compare two resource strings using unsigned bytes, return -1/0/1. |
| `0x81` | `0x00419d00` | I | Read signed byte (resource,offset), return i16. |
| `0x82` | `0x00419d50` | I | Write low byte (resource,offset,value). |
| `0x83` | `0x00419db0` | I | Decimal resource string to signed integer, retaining low word. |
| `0x84` | `0x00419e00` | I | Format signed integer as decimal into resource (resource,value). |
| `0x85` | `0x00419ee0` | I | Read unsigned byte from word memory (address,byteOffset), using little-endian word layout. |
| `0x86` | `0x00419ea0` | I | Write low byte into word memory (address,byteOffset,value), preserving the other byte of the word. |
| `0x87` | `0x00419fb0` | I | Export (word address, byte offset, resource ID, byte count); resize destination resource, copy little-endian memory bytes. Validate source before resize. |
| `0x88` | `0x00419f20` | I | Import (word address, byte offset, resource ID, byte count); read from resource start into word memory, preserving adjacent bytes. Validate both complete spans before writing. |
| `0x89` | `0x0041a050` | I | Substitute the same string resource for each %s: destination, format, source. Bounded width/precision and 255-byte result; see sgs-resource-format.md. |
| `0x8a` | `0x0041a430` | I | Format one scalar argument into destination resource using format resource. |
| `0x8b` | `0x0041a9c0` | I | Format two scalar arguments into destination resource using format resource. |
| `0x8c` | `0x0041aaa0` | I | Format three scalar arguments into destination resource using format resource. |
| `0x8d` | `0x0041ab80` | I | Format four scalar arguments into destination resource using format resource. |
| `0x8e` | `0x0041ac70` | I | Format five scalar arguments into destination resource using format resource, same core 0x41a500. |
| `0x8f` | `0x0041ad70` | M | Begin host input/dialog operation with resource argument; stops timers via 0x41ae10. Exact UI contract unresolved. |
| `0x90` | `0x0041ae30` | P | Play resource audio through original sound wrapper; wrapper type/length precede payload. |
| `0x91` | `0x0040be40` | I | Stop audio. |
| `0x92` | `0x0041aeb0` | I | Pass resource audio data to original empty function 0x40bf80; resource is validated but no effect in inspected runtime. |
| `0x93` | `0x0040bf80` | I | Original empty function, zero arguments, no result. |
| `0x94` | `0x0041af30` | I | Vibrate for duration; delegates original wrapper 0x40bee0. |
| `0x95` | `0x0040bf40` | I | Stop vibration through original host timer cancellation. |
| `0x96` | `0x0041af50` | I | Consume one argument then call original empty function 0x40bf80. Device feature name unresolved. |
| `0x97` | `0x0041af70` | I | Consume one argument then call original empty function 0x40bf80. Device feature name unresolved. |
| `0x98` | `0x0041af90` | I | Load nonvolatile words (address,count). |
| `0x99` | `0x0041afe0` | I | Save nonvolatile words (address,count). |
| `0x9a` | `0x0041b030` | I | Start timer 0 (milliseconds,repeat); minimum accepted interval 10ms. |
| `0x9b` | `0x0041b080` | I | Start timer 1 (milliseconds,repeat); minimum accepted interval 10ms. |
| `0x9c` | `0x0041b0d0` | I | Start timer 2 (milliseconds,repeat); minimum accepted interval 10ms. |
| `0x9d` | `0x0041b120` | I | Stop timer 0. |
| `0x9e` | `0x0041b140` | I | Stop timer 1. |
| `0x9f` | `0x0041b160` | I | Stop timer 2. |
| `0xa0` | `0x0041b180` | I | Sign-extend the seed into the session-local original 32-bit recurrence. See [random services](sgs-random.md). |
| `0xa1` | `0x0041b1a0` | I | Original 15-bit draw modulo the sorted bound difference; equal bounds return without drawing. Wide intervals retain the original distribution. |
| `0xa2` | `0x0041b1f0` | I | Always consume an original 15-bit draw; compare its remainder modulo 100 with the signed percentage. |
| `0xa3` | `0x0041b220` | I | Absolute value with signed-word overflow behavior. |
| `0xa4` | `0x0041b240` | I | Sign: -1,0,1. |
| `0xa5` | `0x0041b270` | I | Sine of integer degrees, result scaled 100 and rounded; reduce angle modulo 360. |
| `0xa6` | `0x0041b340` | I | Cosine of integer degrees, result scaled 100 and rounded; reduce angle modulo 360. |
| `0xa7` | `0x0041b410` | I | Tangent of integer degrees, result scaled 100 and rounded; modulo 180; exactly 90 returns 0. |
| `0xa8` | `0x0041b490` | I | Arcsine(value/100), rounded integer degrees; domain -100..100, otherwise 32767. |
| `0xa9` | `0x0041b4f0` | I | Arccosine(value/100), rounded integer degrees; domain -100..100, otherwise 32767. |
| `0xaa` | `0x0041b550` | I | Arctangent(value/100), rounded integer degrees; original accepts -99..100 only, otherwise 32767. |
| `0xab` | `0x0041b5b0` | I | Mean of two signed words using widened sum and truncation toward zero. |
| `0xac` | `0x0041b5e0` | I | Mean of three signed words using widened sum and truncation toward zero. |
| `0xad` | `0x0041b630` | I | Maximum of two signed words. |
| `0xae` | `0x0041b670` | I | Maximum of three signed words. |
| `0xaf` | `0x0041b6d0` | I | Minimum of two signed words. |
| `0xb0` | `0x0041b710` | I | Minimum of three signed words. |
| `0xb1` | `0x0041b770` | I | Index of maximum in (address,count); earliest tie wins. |
| `0xb2` | `0x0041b7e0` | I | Index of minimum in (address,count); earliest tie wins. |
| `0xb3` | `0x0041b850` | I | Index of nearest value in (address,count,target); original exact-match and tie behavior needs preserved tests. |
| `0xb4` | `0x0041baf0` | I | Scalar vector operation (destination,scalar,count,operation). Twelve operators, forward overlap, original arithmetic faults; complete range/work preflight. See [vector evidence](sgs-vectors.md). |
| `0xb5` | `0x0041bdd0` | I | In-place vector operation (destination,source,count,operation). Twelve operators, forward overlap, original arithmetic faults; complete range/work preflight. See [vector evidence](sgs-vectors.md). |
| `0xb6` | `0x0041c110` | I | Three-address vector operation (destination,leftSource,rightSource,count,operation). Twelve operators, forward overlap, original arithmetic faults; complete range/work preflight. See [vector evidence](sgs-vectors.md). |
| `0xb7` | `0x0041c1a0` | I | Exclusive range test (center,value,radius): center-radius < value < center+radius. |
| `0xb8` | `0x0041c200` | I | Write date (year,month,day,weekday) to four words at popped address. |
| `0xb9` | `0x0041c240` | I | Write time fields to four words at popped address; millisecond fourth field uses host clock. |
| `0xba` | `0x0041c280` | I | Set original overlay policy byte. |
| `0xbb` | `0x0041c2a0` | M | Toggle/set original overlay state, consumes one argument; policy can suppress transition. |
| `0xbc` | `0x0041c3b0` | M | Begin mode-dependent communication request from (resource,value), yields current invocation. |
| `0xbd` | `0x0041c4a0` | M | Encode word-memory bytes in an SG-prefixed communication packet; (address,length), cap payload 249, write result to system variable 0. |
| `0xbe` | `0x0041c4f0` | M | Two-argument mode-dependent communication helper 0x410f10; returns 0 when runtime mode !=3. |
| `0xbf` | `0x0041c540` | M | One-argument communication helper 0x410fe0; replaces argument with return value. |
| `0xc0` | `0x0041c570` | M | Communication/download command using word-addressed data and other scalar arguments; calls 0x40c3d0. Exact argument order unresolved. |
| `0xc1` | `0x0041c5c0` | M | Resource download helper using address/length, calls 0x40c540. Exact completion behavior unresolved. |
| `0xc2` | `0x0041c600` | M | SMS helper (type,dialResource,textResource), replaces three args with result from 0x40c680. |
| `0xc3` | `0x0041c690` | M | Communication request using multiple resource descriptors and 0x412400; exact wire semantics unresolved. |
| `0xc4` | `0x0041c750` | I | Request external URL launch from resource; current host explicitly rejects it. |
| `0xc5` | `0x0041c7c0` | M | Set host action 2 and redirect PC to invocation terminator; exit/return-to-shell effect requires host-action verification. |
| `0xc6` | `0x0041c7e0` | M | Device/system query with multiple resource destinations through 0x40c9c0; exact argument layout unresolved. |
| `0xc7` | `0x0041cb40` | M | Resource-based host operation through 0x40ca50; updates system variable 0 with result. |
| `0xc8` | `0x0041cb90` | I | Set clipping rectangle (x1,y1,x2,y2), sorted/clamped inclusive endpoints. |
| `0xc9` | `0x0041cbd0` | I | Reset clipping rectangle to full framebuffer. |
| `0xca` | `0x0041cbe0` | I | Read raw framebuffer byte (x,y); outside screen returns -1. |
| `0xcb` | `0x0041cc20` | I | Rounded outline (x1,y1,x2,y2,radius), sorted inclusive endpoints, absolute radius clamped to half each span; integer midpoint corners and straight edges. |
| `0xcc` | `0x0041cc70` | I | Filled rounded rectangle with the same bounds/radius contract; preserves the original normalized middle-interval behavior, including zero-height expansion. |
| `0xcd` | `0x0041ccc0` | I | Invert every raw RGB332 byte in sorted inclusive rectangle (x1,y1,x2,y2), intersected with current clip. |
| `0xce` | `0x0041cd00` | I | Whole-buffer scroll (buffer,dx,dy,mode), white-fill or wrap. Invalid selectors are no-ops; oversized white-fill retains the drawing-buffer quirk. See sgs-scroll.md. |
| `0xcf` | `0x0041cd40` | I | Extended transparent text (x,y,resource,flags): bold, italic and underline; see verified raster contracts in sgs-text-effects.md. |
| `0xd0` | `0x0041cdd0` | I | Extended text with background, same four operands and effect mask as cf; atomic glyph/work preflight. |
| `0xd1` | `0x0041ce60` | M | Host dialog/action involving three resource IDs, stopping timers and yielding; exact UI behavior unresolved. |
| `0xd2` | `0x0041cf70` | I | Push runtime mode byte. Standalone startup exposes mode 2 through this service and system variable 0 during initialization. |
| `0xd3` | `0x0041c3b0` | M | Alias of bc, identical original handler address. |
| `0xd4` | `0x0041cfa0` | M | Stop timers and emit host action 8 or 9 based on runtime/device mode; yields. |
| `0xd5` | `0x0041cff0` | M | Host operation (resource,byteValue), actions 11/12 depending mode; yields. |
| `0xd6` | `0x0041d0a0` | M | Host operation (resource), actions 13/16 depending mode; yields. |
| `0xd7` | `0x0041d120` | M | No-argument host operation, actions 14/16 depending mode; yields. |
| `0xd8` | `0x0041d170` | M | No-argument host operation, actions 15/16 depending mode; yields. |
| `0xd9` | `0x0041d1c0` | M | No-argument host operation conditional on mode 2; exact action unresolved. |
| `0xda` | `0x0041d210` | M | Communication/download operation using 0x411c50,0x40b0e0,0x40b8f0,0x412400; argument protocol unresolved. |
| `0xdb` | `0x0041d330` | I | Original implemented no-op consuming two words. |
| `0xdc` | `0x0041d340` | I | Original implemented no-op consuming one word. |
| `0xdd` | `0x0041d350` | I | Original implemented no-op consuming one word. |
| `0xde` | `0x0041d360` | M | Resource-based host operation through 0x40cae0; exact contract unresolved. |
| `0xdf` | `0x0041d3d0` | M | Resource-based host operation through 0x40cb80; exact contract unresolved. |
| `0xe0` | `0x0041d440` | I | Consumes six words and returns nothing. Conditional device wrapper calls the empty original helper at 0x40bf80; no host effect. |
| `0xe1` | `0x0041d490` | I | Replace top with 0 if input ==1, otherwise -1; original capability probe. |
| `0xe2` | `0x0041d4c0` | M | Seven words: (1,1,decodedObjectIndex,x,y,unused,unused), returning 0/-1. Draws a previously decoded SAF object. See [extended images](sgs-images.md). |
| `0xe3` | `0x0041d540` | I | Reserved original empty handler, no arguments or results. |
| `0xe4` | `0x0041d540` | I | Reserved original empty handler, no arguments or results. |
| `0xe5` | `0x0041d540` | I | Reserved original empty handler, no arguments or results. |
| `0xe6` | `0x0041d550` | M | Seven words: (1,suboperation,resource,p,q,unused,unused). Initialize SIS/SAF decoder, decode SAF object, or render decoded pixels. See [extended images](sgs-images.md). |
| `0xe7` | `0x0041d6c0` | P | Seven words: (1,1,sourceResource,destinationResource,frameIndex,unused,unused). Extract type-1 literal/coded SIS frames with exact object references into a resource; reference transforms, other composition modes, type 2 and SAF remain unsupported. See [extended images](sgs-images.md). |
| `0xe8` | `0x0041d830` | P | Local selector (1,1) now queries SIS headers and writes five words; focused tests cover the query; native comparison remains follow-up. SAF metadata and host selector 3 remain unsupported. See [pause status](sgs-completion.md#paused-at-user-request). |
| `0xe9` | `0x0041d540` | I | Reserved original empty handler, no arguments or results. |
| `0xea` | `0x0041d540` | I | Reserved original empty handler, no arguments or results. |
| `0xeb` | `0x0041d540` | I | Reserved original empty handler, no arguments or results. |
| `0xec` | `0x0041d540` | I | Reserved original empty handler, no arguments or results. |
| `0xed` | `0x0041d540` | I | Reserved original empty handler, no arguments or results. |
| `0xee` | `0x0041d540` | I | Reserved original empty handler, no arguments or results. |
| `0xef` | `0x0041d540` | I | Reserved original empty handler, no arguments or results. |
| `0xf0` | `0x0041db80` | I | Diagnostic trace with format resource and three signed words; no framebuffer change, routed through host debug logger. |
| `0xf1` | `0x0041dc10` | I | Diagnostic trace with format resource and string resource, repeated %s substitution; no framebuffer change. |
| `0xf2` | `0x0041d540` | I | Reserved original empty handler, no arguments or results. |

## Mathematical derivations without copied tables

The ordinary palette cube is generated from RGB332 red levels 0,2,4,7, green levels 0..7 and blue levels 0..3, excluding the four gray entries already represented by script color constants. Seven brightness banks are derived by integer channel transforms in `scriptPaletteBank`. Exhaustive local comparison of all 182 entries across seven original banks produced 1,274 matches; no table data is needed by the implementation. Decorative aliases and palette schemes need their own evidence rather than assuming cube membership.

Trigonometric tables can likewise be replaced with independently evaluated formulas. For integer angles 0..90, rounding 100 times sine/cosine/tangent reproduces all 273 inspected entries, with tangent at 90 explicitly zero. For values 0..100, rounding inverse-function degrees reproduces all 303 inspected inverse entries. The independent trig helper now checks all 65,536 signed-word inputs for each forward or inverse service: forward periodicity, symmetry and bounds; inverse domains, monotonicity and round-trip precision. Golden quadrant and inverse endpoint cases pin the scale and special sentinels. Host dispatch wiring is tracked separately in the rows above.

## Next contract boundaries

- Sprite queue opcodes 0x73..0x75 have authored stack-save/restore, ordering, capacity and storage-lifetime tests. Native pointer byte aliases remain explicitly unavailable.
- Vector operations 0xb4..0xb6 have verified argument order, overlap, arithmetic faults and bounded work; see [the completed contract audit](sgs-vectors.md).
- Communication and host UI services require event/result semantics. A missing network or dialog implementation must not become a silent success.
- Extended image services need independently authored image fixtures, resource lifetime rules and validated dimensions before decoder integration.
- Reserved original no-ops can be accepted only with their verified stack effects; empty implementations elsewhere do not justify classifying an unrelated missing opcode as reserved.
