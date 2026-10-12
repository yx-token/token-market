package quality

import (
	"fmt"
	"testing"
)

// 以下压力校验保留精简前的所有 seed 及全部独立求解、唯一解、判分和确定性断言。
// 显式运行：go test -race -timeout 20m ./internal/quality -run '^$' -bench '(ZebraIndependentUnique|FreeWritingIndependentValidation|LongContextIndependentSolutions)Stress$' -benchtime=1x
func BenchmarkBankZebraIndependentUniqueStress(b *testing.B) {
	for _, dimensions := range bankZebraTestDimensions {
		b.Run(fmt.Sprintf("%dx%d", dimensions[0], dimensions[1]), func(b *testing.B) {
			b.ReportAllocs()
			for iteration := 0; iteration < b.N; iteration++ {
				for seed := uint64(1); seed <= 6; seed++ {
					assertBankZebraIndependentUnique(b, dimensions, seed)
				}
			}
		})
	}
}

func BenchmarkBankFreeWritingIndependentValidationStress(b *testing.B) {
	for _, lang := range []string{"zh", "en"} {
		b.Run(lang, func(b *testing.B) {
			b.ReportAllocs()
			for iteration := 0; iteration < b.N; iteration++ {
				for seed := uint64(0); seed < 100; seed++ {
					assertBankFreeWritingIndependentValidation(b, lang, seed)
				}
			}
		})
	}
}

func BenchmarkBankLongContextIndependentSolutionsStress(b *testing.B) {
	for _, target := range bankContextTestTargets {
		for _, variant := range bankContextTestVariants {
			b.Run(fmt.Sprintf("%s/%d", variant, target), func(b *testing.B) {
				b.ReportAllocs()
				for iteration := 0; iteration < b.N; iteration++ {
					for seed := uint64(0); seed < 3; seed++ {
						assertBankLongContextIndependentSolution(b, target, variant, seed)
					}
				}
			})
		}
	}
}
