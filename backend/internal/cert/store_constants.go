// Copyright 2025 The ThunderID Authors
// SPDX-License-Identifier: Apache-2.0

package cert

import dbmodel "github.com/thunder-id/thunderid/internal/system/database/model"

var (
	// queryGetCertificateByID retrieves a certificate by its ID.
	queryGetCertificateByID = dbmodel.DBQuery{
		ID: "CER_MGT-01",
		Query: `SELECT ID, REF_TYPE, REF_ID, TYPE, VALUE FROM "CERTIFICATE" ` +
			`WHERE ID = $1 AND DEPLOYMENT_ID = $2`,
	}
	// queryGetCertificateByReference retrieves a certificate based on its reference type and ID.
	queryGetCertificateByReference = dbmodel.DBQuery{
		ID: "CER_MGT-02",
		Query: `SELECT ID, REF_TYPE, REF_ID, TYPE, VALUE FROM "CERTIFICATE" ` +
			`WHERE REF_TYPE = $1 AND REF_ID = $2 AND DEPLOYMENT_ID = $3`,
	}
	// queryInsertCertificate is the query to insert a certificate into the database.
	queryInsertCertificate = dbmodel.DBQuery{
		ID: "CER_MGT-03",
		Query: `INSERT INTO "CERTIFICATE" (ID, REF_TYPE, REF_ID, TYPE, VALUE, DEPLOYMENT_ID) ` +
			`VALUES ($1, $2, $3, $4, $5, $6)`,
	}
	// queryUpdateCertificateByID updates a certificate based on its ID.
	queryUpdateCertificateByID = dbmodel.DBQuery{
		ID: "CER_MGT-04",
		Query: `UPDATE "CERTIFICATE" SET TYPE = $2, VALUE = $3, UPDATED_AT = CURRENT_TIMESTAMP ` +
			`WHERE ID = $1 AND DEPLOYMENT_ID = $4`,
	}
	// queryUpdateCertificateByReference updates a certificate based on its reference type and ID.
	queryUpdateCertificateByReference = dbmodel.DBQuery{
		ID: "CER_MGT-05",
		Query: `UPDATE "CERTIFICATE" SET TYPE = $3, VALUE = $4, UPDATED_AT = CURRENT_TIMESTAMP ` +
			`WHERE REF_TYPE = $1 AND REF_ID = $2 AND DEPLOYMENT_ID = $5`,
	}
	// queryDeleteCertificateByID deletes a certificate by its ID.
	queryDeleteCertificateByID = dbmodel.DBQuery{
		ID:    "CER_MGT-06",
		Query: `DELETE FROM "CERTIFICATE" WHERE ID = $1 AND DEPLOYMENT_ID = $2`,
	}
	// queryDeleteCertificateByReference deletes a certificate by its reference type and ID.
	queryDeleteCertificateByReference = dbmodel.DBQuery{
		ID:    "CER_MGT-07",
		Query: `DELETE FROM "CERTIFICATE" WHERE REF_TYPE = $1 AND REF_ID = $2 AND DEPLOYMENT_ID = $3`,
	}
)
