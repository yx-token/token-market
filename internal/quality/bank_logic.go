package quality

func init() {
	bankGenerators["zebra"] = generateZebra
	bankGenerators["knights_knaves"] = generateKnights
	bankGenerators["register_machine"] = generateRegister
}
