package platform

type NativeFlowAssets interface {
	NativeFlowAsset(Manifest, string, int, []AssetRef) (ReleaseAsset, error)
}
