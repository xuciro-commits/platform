package platform

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"time"
)

type TxnLink struct {
	Binding AssetBinding `json:"binding"`
	Parent  TxnRef       `json:"parent"`
	Child   TxnRef       `json:"child"`
}

func (c Caller) PutOwnedRelation(receipt *pb.ChangeRecord, link TxnLink, remove bool, at time.Time) *Error {
	runtime := c.StagedDecision()
	if runtime == nil {
		return Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Owned relation writes require a private host")
	}
	return runtime.PutOwnedRelation(c, receipt, link, remove, at)
}

func (c Caller) MigrateOwnedRelations(receipt *pb.ChangeRecord, binding AssetBinding, at time.Time) *Error {
	runtime := c.StagedDecision()
	if runtime == nil {
		return Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Relationship migration requires a private decision")
	}
	return runtime.MigrateOwnedRelations(c, receipt, binding, at)
}

func (c Caller) OwnedRelations(entity string) []RelationLink {
	if c.rt != nil {
		return c.rt.OwnedRelations(c, entity)
	}
	return nil
}

func (c Caller) LegacyRelationActive(link RelationLink) bool {
	if c.rt != nil {
		return c.rt.LegacyRelationActive(c, link)
	}
	return true
}

func (c Caller) LegacyRelationWritable(from, to string) bool {
	if c.rt != nil {
		return c.rt.LegacyRelationWritable(c, from, to)
	}
	return true
}
