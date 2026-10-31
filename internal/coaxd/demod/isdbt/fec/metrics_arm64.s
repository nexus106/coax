//go:build arm64 && !purego

#include "textflag.h"

// Four even and four odd survivor masks are reduced with weights 1,4,16,64
// and 2,8,32,128, producing eight history bits per vector group.
DATA metricWeights<>+0(SB)/4, $1
DATA metricWeights<>+4(SB)/4, $4
DATA metricWeights<>+8(SB)/4, $16
DATA metricWeights<>+12(SB)/4, $64
GLOBL metricWeights<>(SB), RODATA|NOPTR, $16

TEXT ·updateMetrics(SB), NOSPLIT, $0-40
 MOVD current+0(FP), R0
 MOVD next+8(FP), R1
 MOVD costs+16(FP), R2
 MOVW total+24(FP), R3
 ADD $128, R0, R4
 MOVD $metricWeights<>(SB), R6
 VLD1 (R6), [V16.S4]
 VDUP R3, V17.S4
 MOVD $0, R5
 MOVD $0, R7
 MOVD $8, R8
loop:
 VLD1.P 16(R0), [V0.S4]
 VLD1.P 16(R4), [V1.S4]
 VLD1.P 16(R2), [V2.S4]
 VSUB V2.S4, V17.S4, V3.S4
 VADD V2.S4, V0.S4, V4.S4
 VADD V3.S4, V1.S4, V5.S4
 VADD V3.S4, V0.S4, V6.S4
 VADD V2.S4, V1.S4, V7.S4
 VSMIN V5.S4, V4.S4, V8.S4
 VSMIN V7.S4, V6.S4, V9.S4
 VZIP1 V9.S4, V8.S4, V12.S4
 VZIP2 V9.S4, V8.S4, V13.S4
 VST1.P [V12.S4,V13.S4], 32(R1)
 VCMGT V5.S4, V4.S4, V10.S4
 VCMGT V7.S4, V6.S4, V11.S4
 VAND V16.B16, V10.B16, V10.B16
 VAND V16.B16, V11.B16, V11.B16
 VSHL $1, V11.S4, V11.S4
 VADD V11.S4, V10.S4, V10.S4
 VADDV V10.S4, V10
 VMOV V10.S[0], R9
 LSL R7, R9, R9
 ORR R9, R5, R5
 ADD $8, R7, R7
 SUBS $1, R8, R8
 BNE loop
 MOVD R5, ret+32(FP)
 RET
