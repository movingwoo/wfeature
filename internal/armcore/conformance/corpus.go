package conformance

import "github.com/movingwoo/wfeature/internal/armcore"

// The corpus.
//
// Every case is one architectural rule, and the rules chosen are the ones an
// execution strategy gets wrong: state that has to survive an instruction which
// does not write it, shift amounts an encoding cannot hold as themselves, what
// a multiply does to the flags it does not define, what R15 is worth when it is
// read as data, a base register that also appears in a transfer list, and the
// two instruction sets meeting.
//
// A case's Want is worked out from the rule and written down before it is run.

// orrImmediate encodes ORR<cond> Rd, Rn, #imm8. It is written out because the
// condition-code case below needs fourteen of them, and fourteen hand-written
// hex constants differing in four bits is a place to hide a typo rather than a
// place to prove one is not there. The layout: cond, 001 (immediate operand),
// 1100 (ORR), S=0, Rn, Rd, and a rotate-zero 8-bit immediate.
func orrImmediate(condition, rd, rn, immediate uint32) uint32 {
	return condition<<28 | 0x03800000 | rn<<16 | rd<<12 | immediate&0xff
}

// The condition codes, in their encoding order.
const (
	condEQ uint32 = iota
	condNE
	condCS
	condCC
	condMI
	condPL
	condVS
	condVC
	condHI
	condLS
	condGE
	condLT
	condGT
	condLE
)

// registers is a small helper so a case can name only the registers it sets.
func registers(pairs map[int]uint32) [15]uint32 {
	var state [15]uint32
	for index, value := range pairs {
		state[index] = value
	}
	return state
}

func expected(pairs map[int]uint32) [16]uint32 {
	var state [16]uint32
	for index, value := range pairs {
		state[index] = value
	}
	return state
}

// Cases is the whole corpus.
func Cases() []Case {
	return []Case{
		{
			Name: "carry survives an instruction that does not write it",
			Rule: "only the S form of a data-processing instruction writes the flags, " +
				"so a carry set by ADDS is still there for the ADC that reads it",
			Registers: registers(map[int]uint32{0: 0xffffffff, 1: 1}),
			Image: arm(
				0xe0902001, // adds r2, r0, r1   -> r2 = 0, C = 1, Z = 1
				0xe3a03005, // mov  r3, #5       -> writes no flag
				0xe2a34000, // adc  r4, r3, #0   -> r4 = 5 + 0 + C
			),
			Want: Snapshot{
				Registers: expected(map[int]uint32{
					0: 0xffffffff, 1: 1, 2: 0, 3: 5, 4: 6, 15: CodeBase + 12,
				}),
				CPSR:   ModeUser | FlagZ | FlagC,
				Reason: armcore.StopEnd,
				Steps:  3,
			},
		},
		{
			Name: "an ADC chain carries between the halves of a 64-bit add",
			Rule: "ADDS writes the carry out of the low half and ADC adds it into the high half",
			Registers: registers(map[int]uint32{
				0: 0xffffffff, 1: 0x00000001, 2: 0x00000001, 3: 0x00000002,
			}),
			Image: arm(
				0xe0904002, // adds r4, r0, r2   -> r4 = 0, C = 1
				0xe0a15003, // adc  r5, r1, r3   -> r5 = 1 + 2 + 1
			),
			Want: Snapshot{
				Registers: expected(map[int]uint32{
					0: 0xffffffff, 1: 1, 2: 1, 3: 2, 4: 0, 5: 4, 15: CodeBase + 8,
				}),
				CPSR:   ModeUser | FlagZ | FlagC,
				Reason: armcore.StopEnd,
				Steps:  2,
			},
		},
		{
			Name: "LSR by an immediate zero means thirty-two",
			Rule: "the five-bit shift field cannot hold 32, so LSR #0 encodes LSR #32: " +
				"the result is zero and the carry is bit 31 of the operand",
			Registers: registers(map[int]uint32{1: 0x80000000}),
			Image: arm(
				0xe1b00021, // movs r0, r1, lsr #32
			),
			Want: Snapshot{
				Registers: expected(map[int]uint32{1: 0x80000000, 15: CodeBase + 4}),
				CPSR:      ModeUser | FlagZ | FlagC,
				Reason:    armcore.StopEnd,
				Steps:     1,
			},
		},
		{
			Name: "ASR by an immediate zero means thirty-two",
			Rule: "ASR #0 encodes ASR #32: every bit of the result is bit 31 of the operand, " +
				"and so is the carry",
			Registers: registers(map[int]uint32{1: 0x80000000}),
			Image: arm(
				0xe1b00041, // movs r0, r1, asr #32
			),
			Want: Snapshot{
				Registers: expected(map[int]uint32{0: 0xffffffff, 1: 0x80000000, 15: CodeBase + 4}),
				CPSR:      ModeUser | FlagN | FlagC,
				Reason:    armcore.StopEnd,
				Steps:     1,
			},
		},
		{
			Name: "RRX rotates the carry in and bit zero out",
			Rule: "ROR #0 encodes RRX: a 33-bit rotate through the carry, " +
				"so the result takes the old carry as its top bit and the carry takes bit 0",
			Registers: registers(map[int]uint32{1: 0x00000003}),
			Flags:     FlagC,
			Image: arm(
				0xe1b00061, // movs r0, r1, rrx
			),
			Want: Snapshot{
				Registers: expected(map[int]uint32{0: 0x80000001, 1: 3, 15: CodeBase + 4}),
				CPSR:      ModeUser | FlagN | FlagC,
				Reason:    armcore.StopEnd,
				Steps:     1,
			},
		},
		{
			Name:      "a register LSL of exactly thirty-two keeps bit zero as the carry",
			Rule:      "a register-specified LSL of 32 gives a zero result and bit 0 of the operand as the carry",
			Registers: registers(map[int]uint32{1: 0x00000003, 2: 32}),
			Image: arm(
				0xe1b00211, // movs r0, r1, lsl r2
			),
			Want: Snapshot{
				Registers: expected(map[int]uint32{1: 3, 2: 32, 15: CodeBase + 4}),
				CPSR:      ModeUser | FlagZ | FlagC,
				Reason:    armcore.StopEnd,
				Steps:     1,
			},
		},
		{
			Name:      "a register LSL past thirty-two clears the carry too",
			Rule:      "a register-specified LSL of more than 32 shifts every bit out, carry included",
			Registers: registers(map[int]uint32{1: 0x00000003, 2: 33}),
			Flags:     FlagC,
			Image: arm(
				0xe1b00211, // movs r0, r1, lsl r2
			),
			Want: Snapshot{
				Registers: expected(map[int]uint32{1: 3, 2: 33, 15: CodeBase + 4}),
				CPSR:      ModeUser | FlagZ,
				Reason:    armcore.StopEnd,
				Steps:     1,
			},
		},
		{
			Name:      "a register shift of zero leaves the carry alone",
			Rule:      "a register-specified shift of zero passes the operand through and does not touch the carry",
			Registers: registers(map[int]uint32{1: 0x00000003, 2: 0}),
			Flags:     FlagC,
			Image: arm(
				0xe1b00211, // movs r0, r1, lsl r2
			),
			Want: Snapshot{
				Registers: expected(map[int]uint32{0: 3, 1: 3, 15: CodeBase + 4}),
				CPSR:      ModeUser | FlagC,
				Reason:    armcore.StopEnd,
				Steps:     1,
			},
		},
		{
			Name:      "MULS writes N and Z and leaves C and V alone",
			Rule:      "a multiply defines only N and Z; the carry and overflow a caller was holding survive it",
			Registers: registers(map[int]uint32{1: 0xffffffff, 2: 2}),
			Flags:     FlagC | FlagV,
			Image: arm(
				0xe0100291, // muls r0, r1, r2
			),
			Want: Snapshot{
				Registers: expected(map[int]uint32{
					0: 0xfffffffe, 1: 0xffffffff, 2: 2, 15: CodeBase + 4,
				}),
				CPSR:   ModeUser | FlagN | FlagC | FlagV,
				Reason: armcore.StopEnd,
				Steps:  1,
			},
		},
		{
			Name: "every condition code answers the flags a compare left",
			Rule: "1 - 2 leaves N set, Z clear, C clear (the subtract borrowed) and V clear, " +
				"which passes NE, CC, MI, VC, LS, LT and LE and fails the other seven",
			Registers: registers(map[int]uint32{0: 1, 1: 2}),
			Image: arm(
				0xe1500001, // cmp r0, r1
				orrImmediate(condEQ, 5, 5, 1<<0),
				orrImmediate(condNE, 5, 5, 1<<1),
				orrImmediate(condCS, 5, 5, 1<<2),
				orrImmediate(condCC, 5, 5, 1<<3),
				orrImmediate(condMI, 5, 5, 1<<4),
				orrImmediate(condPL, 5, 5, 1<<5),
				orrImmediate(condVS, 5, 5, 1<<6),
				orrImmediate(condVC, 5, 5, 1<<7),
				orrImmediate(condHI, 6, 6, 1<<0),
				orrImmediate(condLS, 6, 6, 1<<1),
				orrImmediate(condGE, 6, 6, 1<<2),
				orrImmediate(condLT, 6, 6, 1<<3),
				orrImmediate(condGT, 6, 6, 1<<4),
				orrImmediate(condLE, 6, 6, 1<<5),
			),
			Want: Snapshot{
				Registers: expected(map[int]uint32{
					0: 1, 1: 2,
					5:  1<<1 | 1<<3 | 1<<4 | 1<<7,
					6:  1<<1 | 1<<3 | 1<<5,
					15: CodeBase + 60,
				}),
				CPSR:   ModeUser | FlagN,
				Reason: armcore.StopEnd,
				// A condition that fails still costs an instruction: the count is
				// what the guest retired, not what it did.
				Steps: 15,
			},
		},
		{
			Name: "STM with the base first in the list stores the base it started with",
			Rule: "the base is written back before the transfers, and a base that is the lowest " +
				"register in the list is still stored with its original value",
			Registers: registers(map[int]uint32{0: ScratchBase, 1: 0x11, 2: 0x22}),
			Image: arm(
				0xe8a00007, // stmia r0!, {r0, r1, r2}
			),
			Want: Snapshot{
				Registers: expected(map[int]uint32{
					0: ScratchBase + 12, 1: 0x11, 2: 0x22, 15: CodeBase + 4,
				}),
				Scratch: words(ScratchBase, 0x11, 0x22),
				CPSR:    ModeUser,
				Reason:  armcore.StopEnd,
				Steps:   1,
			},
		},
		{
			Name: "LDM with the base in the list keeps the loaded value, not the writeback",
			Rule: "the writeback happens first and the load then overwrites it, " +
				"so the base ends the instruction holding what memory gave it",
			Registers: registers(map[int]uint32{0: ScratchBase}),
			Scratch:   words(0x0000aaaa, 0x0000bbbb),
			Image: arm(
				0xe8b00003, // ldmia r0!, {r0, r1}
			),
			Want: Snapshot{
				Registers: expected(map[int]uint32{
					0: 0x0000aaaa, 1: 0x0000bbbb, 15: CodeBase + 4,
				}),
				Scratch: words(0x0000aaaa, 0x0000bbbb),
				CPSR:    ModeUser,
				Reason:  armcore.StopEnd,
				Steps:   1,
			},
		},
		{
			Name: "R15 reads as the instruction plus eight in ARM state",
			Rule: "the pipeline puts R15 two instructions ahead when it is read as an operand",
			Image: arm(
				0xe1a0000f, // mov r0, pc      -> the address of this instruction plus 8
				0xe28f1004, // add r1, pc, #4  -> the address of this instruction plus 12
			),
			Want: Snapshot{
				Registers: expected(map[int]uint32{
					0: CodeBase + 8, 1: CodeBase + 4 + 8 + 4, 15: CodeBase + 8,
				}),
				CPSR:   ModeUser,
				Reason: armcore.StopEnd,
				Steps:  2,
			},
		},
		{
			Name:      "STM stores R15 as the instruction plus twelve",
			Rule:      "a store-multiple is a cycle later than an operand read, so R15 goes out as PC + 12",
			Registers: registers(map[int]uint32{0: ScratchBase}),
			Image: arm(
				0xe8808000, // stmia r0, {pc}
			),
			Want: Snapshot{
				Registers: expected(map[int]uint32{0: ScratchBase, 15: CodeBase + 4}),
				Scratch:   words(CodeBase + 12),
				CPSR:      ModeUser,
				Reason:    armcore.StopEnd,
				Steps:     1,
			},
		},
		{
			Name: "BX crosses into Thumb and back on bit zero of the target",
			Rule: "bit 0 of a BX target selects the instruction set and is not kept in the PC",
			Image: join(
				arm(
					0xe28f0005, // add r0, pc, #5  -> 0x1008 + 5, a Thumb entry at 0x100c
					0xe28fe004, // add lr, pc, #4  -> the ARM address to come back to
					0xe12fff10, // bx  r0
				),
				thumb(
					0x2107, // movs r1, #7
					0x4770, // bx   lr
				),
				arm(
					0xe3a02009, // mov r2, #9
				),
			),
			Want: Snapshot{
				Registers: expected(map[int]uint32{
					0: CodeBase + 8 + 5, 1: 7, 2: 9,
					armcore.RegisterLR: CodeBase + 0x10,
					15:                 CodeBase + 0x14,
				}),
				CPSR:   ModeUser,
				Reason: armcore.StopEnd,
				Steps:  6,
			},
		},
		{
			Name: "the transfer widths sign-extend and truncate as the encoding says",
			Rule: "LDRB and LDRH zero-extend, LDRSB and LDRSH sign-extend, " +
				"and a byte or halfword store writes only its own width",
			Scratch: []byte{0x81, 0x92, 0xa3, 0xb4},
			Image: arm(
				0xe3a00a02, // mov   r0, #0x2000
				0xe5d01000, // ldrb  r1, [r0]
				0xe1d020b0, // ldrh  r2, [r0]
				0xe1d030d0, // ldrsb r3, [r0]
				0xe1d040f0, // ldrsh r4, [r0]
				0xe5905000, // ldr   r5, [r0]
				0xe5c05004, // strb  r5, [r0, #4]
				0xe1c050b6, // strh  r5, [r0, #6]
			),
			Want: Snapshot{
				Registers: expected(map[int]uint32{
					0: ScratchBase,
					1: 0x81, 2: 0x9281, 3: 0xffffff81, 4: 0xffff9281, 5: 0xb4a39281,
					15: CodeBase + 32,
				}),
				Scratch: []byte{0x81, 0x92, 0xa3, 0xb4, 0x81, 0x00, 0x81, 0x92},
				CPSR:    ModeUser,
				Reason:  armcore.StopEnd,
				Steps:   8,
			},
		},
		{
			Name: "post-indexed and pre-indexed writeback move the base at different times",
			Rule: "a post-indexed transfer uses the base and then moves it; " +
				"a pre-indexed one with writeback moves it first and keeps the move",
			Scratch: words(0x11, 0x22, 0x33),
			Image: arm(
				0xe3a00a02, // mov r0, #0x2000
				0xe4901004, // ldr r1, [r0], #4
				0xe5b02004, // ldr r2, [r0, #4]!
			),
			Want: Snapshot{
				Registers: expected(map[int]uint32{
					0: ScratchBase + 8, 1: 0x11, 2: 0x33, 15: CodeBase + 12,
				}),
				Scratch: words(0x11, 0x22, 0x33),
				CPSR:    ModeUser,
				Reason:  armcore.StopEnd,
				Steps:   3,
			},
		},
		{
			Name: "a supervisor call ends the run at the instruction after it",
			Rule: "the call is retired before the run returns, so the resume point is past it " +
				"and the count includes it",
			Image: arm(
				0xe3a00001, // mov r0, #1
				0xef123456, // svc #0x123456
				0xe3a00002, // mov r0, #2
			),
			Want: Snapshot{
				Registers:         expected(map[int]uint32{0: 1, 15: CodeBase + 8}),
				CPSR:              ModeUser,
				Reason:            armcore.StopSupervisorCall,
				Steps:             2,
				Supervisor:        0x123456,
				SupervisorAddress: CodeBase + 4,
			},
		},
		{
			Name: "a spent budget stops exactly on the instruction it ran out at",
			Rule: "the retired count is the unit a Host paces frames on, so a budget of two " +
				"retires two instructions and leaves the PC on the third",
			Count: 2,
			Image: arm(
				0xe3a00001, // mov r0, #1
				0xe3a01002, // mov r1, #2
				0xe3a02003, // mov r2, #3
			),
			Want: Snapshot{
				Registers: expected(map[int]uint32{0: 1, 1: 2, 15: CodeBase + 8}),
				CPSR:      ModeUser,
				Reason:    armcore.StopCountExhausted,
				Steps:     2,
			},
		},
		{
			Name: "a Thumb shift of thirty-two carries into the ADC that follows",
			Rule: "the Thumb shift immediate reads zero as 32 exactly as the ARM one does, " +
				"and the carry it produces is what the next ADC adds in",
			Thumb:     true,
			Registers: registers(map[int]uint32{1: 0x80000000}),
			Image: thumb(
				0x0808, // lsrs r0, r1, #32
				0x415a, // adcs r2, r3
			),
			Want: Snapshot{
				Registers: expected(map[int]uint32{1: 0x80000000, 2: 1, 15: CodeBase + 4}),
				CPSR:      ModeUser | FlagT,
				Reason:    armcore.StopEnd,
				Steps:     2,
			},
		},
		{
			Name: "R15 reads word-aligned for a Thumb PC-relative form",
			Rule: "a Thumb ADD Rd, PC and a PC-relative load take the PC as the instruction " +
				"plus four with its low two bits cleared",
			Thumb: true,
			End:   CodeBase + 4,
			Image: join(
				thumb(
					0xa100, // add r1, pc, #0
					0x4800, // ldr r0, [pc, #0]
				),
				arm(0xdeadbeef), // the literal both of them point at
			),
			Want: Snapshot{
				Registers: expected(map[int]uint32{
					0: 0xdeadbeef, 1: CodeBase + 4, 15: CodeBase + 4,
				}),
				CPSR:   ModeUser | FlagT,
				Reason: armcore.StopEnd,
				Steps:  2,
			},
		},
	}
}
