package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestNewestFirstOrdersByDateThenID(t *testing.T) {
	require.Len(t, NewestFirst, 2)
	assert.Equal(t, "created_at", NewestFirst[0].Key)
	assert.Equal(t, -1, NewestFirst[0].Value)
	assert.Equal(t, "_id", NewestFirst[1].Key)
	assert.Equal(t, -1, NewestFirst[1].Value)
}

// Sans curseur, le filtre n'est pas touché — pas même un `$and` vide.
func TestAfterNewestLeavesFilterAloneWithoutCursor(t *testing.T) {
	filter := bson.M{"client_id": "c1"}
	require.NoError(t, AfterNewest(t.Context(), nil, filter, primitive.NilObjectID))
	assert.Equal(t, bson.M{"client_id": "c1"}, filter)
}
