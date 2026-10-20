package quality

import (
	"fmt"
	"math/rand/v2"
	"testing"
)

// 每次迭代覆盖原有十个固定 seed；性能量测不重复占用正常与 race 正确性检查。
func BenchmarkBankLogicZebraGenerationDuration(b *testing.B) {
	for _, dimensions := range [][2]int{{5, 5}, {5, 6}, {6, 6}} {
		b.Run(fmt.Sprintf("%dx%d", dimensions[0], dimensions[1]), func(b *testing.B) {
			b.ReportAllocs()
			for iteration := 0; iteration < b.N; iteration++ {
				for seed := uint64(0); seed < 10; seed++ {
					generated, err := generateZebra(rand.New(rand.NewPCG(seed, seed+17)), map[string]any{"houses": dimensions[0], "attributes": dimensions[1]})
					if err != nil {
						b.Fatalf("%dx%d seed=%d：%v", dimensions[0], dimensions[1], seed, err)
					}
					assertBankLogicAnswer(b, generated)
				}
			}
		})
	}
}

// 每次迭代完整验证原有 500 个 seed；用 -run '^$' -bench DefaultGenerationStress -benchtime=1x 显式执行。
func BenchmarkBankLogicDefaultGenerationStress(b *testing.B) {
	for _, generator := range []struct {
		name     string
		generate func(*rand.Rand, map[string]any) (generatedProbe, error)
	}{
		{"zebra", generateZebra}, {"knights", generateKnights}, {"register", generateRegister},
	} {
		b.Run(generator.name, func(b *testing.B) {
			b.ReportAllocs()
			for iteration := 0; iteration < b.N; iteration++ {
				for seed := uint64(0); seed < 500; seed++ {
					generated, err := generator.generate(rand.New(rand.NewPCG(seed, seed+17)), nil)
					if err != nil {
						b.Fatalf("seed=%d 默认域生成失败：%v", seed, err)
					}
					assertBankLogicAnswer(b, generated)
				}
			}
		})
	}
}
