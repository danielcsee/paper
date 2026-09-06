// The graph schema. Idempotent: safe to re-run at any time.
//
// Neo4j Community has no DDL for structure — labels and relationship types
// exist as soon as something writes one. So the only part of the model this
// file can enforce is identity: one uniqueness constraint per node key, plus
// the indexes the read queries need. Everything else — which labels exist,
// which relationship types connect what, which properties are required — is
// enforced by the loader and written down in README.md.
//
// Community also has no node key, property existence, or property type
// constraints; those are Enterprise. Do not add them here expecting them to
// bind.

// ---------- identity ----------

// PMID, not PMCID: PubTator is keyed on PMID and always returns one, while
// pmcid is null for the ~25% of papers that are abstract-only.
CREATE CONSTRAINT paper_pmid IF NOT EXISTS
  FOR (p:Paper) REQUIRE p.pmid IS UNIQUE;

// The namespaced ontology id ("MESH:D001943", "ncbi_gene:672"). Declared on
// :Entity, the shared label, rather than on each of :Gene/:Disease/... — the
// id is unique across every namespace, so one constraint covers them all.
CREATE CONSTRAINT entity_id IF NOT EXISTS
  FOR (e:Entity) REQUIRE e.entity_id IS UNIQUE;

// "surname, given names", casefolded. A placeholder key: see api/graph/keys.py.
CREATE CONSTRAINT author_id IF NOT EXISTS
  FOR (a:Author) REQUIRE a.author_id IS UNIQUE;

// Nothing populates :Claim yet. The constraint is created anyway — it applies
// to an empty label at no cost, and it is the only executable record that the
// label is part of the model. Creating it also registers the label token, so
// :Claim does show up in db.labels() with zero nodes behind it.
CREATE CONSTRAINT claim_id IF NOT EXISTS
  FOR (c:Claim) REQUIRE c.claim_id IS UNIQUE;

// ---------- lookup ----------

CREATE INDEX paper_pub_year IF NOT EXISTS FOR (p:Paper) ON (p.pub_year);
CREATE INDEX entity_name IF NOT EXISTS FOR (e:Entity) ON (e.name);
CREATE INDEX claim_predicate IF NOT EXISTS FOR (c:Claim) ON (c.predicate);

CREATE FULLTEXT INDEX claim_text IF NOT EXISTS FOR (n:Claim) ON EACH [n.text];
