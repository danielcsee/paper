#!/usr/bin/env python3
"""
pmc_oa_downloader.py

Downloads a size-capped subset of full-text papers from the PubMed Central
(PMC) Article Datasets -- legally open, CC-licensed / text-mining-eligible
biomedical full text. NCBI retired the old FTP bulk-download service
(oa_file_list.csv, oa_package/*, the per-ID OA web service) in August 2026;
everything now lives in a public, anonymous-read S3 bucket:

    https://pmc-oa-opendata.s3.amazonaws.com/

How it works:
  1. Streams PMC-ids.csv (the NCBI PMCID/DOI index -- ~12.4M rows, too big
     to hold in memory) row by row instead of downloading a file list.
     PMC-ids.csv has no download path or license info, so it only tells us
     which PMCIDs exist -- not which are open access.
  2. Filters rows by keyword (journal name) and skips embargoed author
     manuscripts (Release Date is a future date instead of "live").
  3. For each surviving candidate, fetches that article's small per-record
     JSON metadata object from the S3 bucket (metadata/PMCID.<version>.json)
     to find out whether it's open access, its license, and the URLs for
     its XML/text/PDF/media files. Most articles are version 1; the rare
     PMCIDs published as version 2+ (no v1 in the bucket) are discovered via
     an S3 ListObjectsV2 call.
  4. Streams the article's files one at a time, tracking cumulative
     downloaded bytes, and stops once you hit your target size.

This uses only the officially supported public dataset (see
https://pmc.ncbi.nlm.nih.gov/tools/pmcaws/) -- no scraping of the PMC
website.

Usage:
    pip install requests
    python paper_downloader.py --target-gb 5 --keyword cancer --out ./pmc_subset

Notes:
  - --keyword matches (case-insensitive) against the "Journal Title" column
    in PMC-ids.csv -- that's the only descriptive text available before an
    article's metadata is fetched, so it's a journal-name filter, not a
    full-text/topic filter. Leave it unset to sample broadly.
  - --license comm restricts to the commercial-use-allowed subset (CC0,
    CC BY, CC BY-SA) using each article's license_code from its metadata.
    Default is "all" (comm + noncomm + text-mining-only manuscripts).
  - Rerunning with the same --out dir resumes (skips files already on disk).
"""

import argparse
import csv
import datetime
import os
import sys
import xml.etree.ElementTree as ET

import requests

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
DEFAULT_PMC_IDS_CSV = os.path.join(SCRIPT_DIR, "PMC-ids.csv")

S3_BASE_URL = "https://pmc-oa-opendata.s3.amazonaws.com"

NONCOMM_LICENSE_CODES = {None, "", "CC BY-NC", "CC BY-NC-SA", "CC BY-NC-ND", "TDM"}


def iter_pmc_ids_rows(csv_path):
    """Stream PMC-ids.csv row by row -- the file is ~1.1GB, never load it whole."""
    print(f"Streaming article index from {csv_path} ...")
    with open(csv_path, newline="", encoding="utf-8") as f:
        yield from csv.DictReader(f)


def matches_keyword(row, keyword):
    if not keyword:
        return True
    return keyword.lower() in row.get("Journal Title", "").lower()


def is_embargoed(row, today):
    release_date = row.get("Release Date", "live")
    if release_date == "live":
        return False
    try:
        return datetime.date.fromisoformat(release_date) > today
    except ValueError:
        return False


def matches_license(metadata, license_filter):
    if license_filter == "all":
        return True
    return metadata.get("license_code") not in NONCOMM_LICENSE_CODES


def s3_url_to_https(s3_url):
    # e.g. "s3://pmc-oa-opendata/PMC13901.1/PMC13901.1.xml?md5=..." -> https, no query
    key = s3_url.split("s3://pmc-oa-opendata/", 1)[1].split("?", 1)[0]
    return f"{S3_BASE_URL}/{key}"


def find_versioned_prefix(pmcid, session):
    """Fall back to an S3 listing when PMCID.1 doesn't exist (rare v2+ only cases)."""
    resp = session.get(
        f"{S3_BASE_URL}/",
        params={"list-type": "2", "prefix": f"{pmcid}.", "delimiter": "/"},
        timeout=30,
    )
    if resp.status_code != 200:
        return None
    ns = {"s3": "http://s3.amazonaws.com/doc/2006-03-01/"}
    root = ET.fromstring(resp.text)
    prefixes = [p.findtext("s3:Prefix", namespaces=ns) for p in root.findall("s3:CommonPrefixes", ns)]
    if not prefixes:
        return None
    return prefixes[0].rstrip("/")  # e.g. "PMC13901.2"


def fetch_metadata(pmcid, session):
    resp = session.get(f"{S3_BASE_URL}/metadata/{pmcid}.1.json", timeout=30)
    if resp.status_code == 200:
        return resp.json()
    if resp.status_code != 404:
        resp.raise_for_status()

    versioned = find_versioned_prefix(pmcid, session)
    if not versioned:
        return None
    resp = session.get(f"{S3_BASE_URL}/metadata/{versioned}.json", timeout=30)
    if resp.status_code != 200:
        return None
    return resp.json()


def download_file(url, local_path, session):
    with session.get(url, stream=True, timeout=60) as r:
        if r.status_code != 200:
            return 0
        size = 0
        with open(local_path, "wb") as f:
            for chunk in r.iter_content(chunk_size=1024 * 256):
                f.write(chunk)
                size += len(chunk)
        return size


def download_subset(rows, out_dir, target_bytes, license_filter, keyword):
    os.makedirs(out_dir, exist_ok=True)
    session = requests.Session()
    today = datetime.date.today()

    total = 0
    kept = 0
    skipped_existing = 0

    for row in rows:
        if total >= target_bytes:
            break
        if not matches_keyword(row, keyword):
            continue
        if is_embargoed(row, today):
            continue

        pmcid = row["PMCID"].strip()
        if not pmcid:
            continue

        try:
            metadata = fetch_metadata(pmcid, session)
        except requests.RequestException as e:
            print(f"  skip (network error fetching metadata): {pmcid} ({e})")
            continue

        if not metadata or not metadata.get("is_pmc_openaccess"):
            continue
        if not matches_license(metadata, license_filter):
            continue

        article_dir = os.path.join(out_dir, pmcid)
        file_urls = [metadata["xml_url"], metadata["text_url"]]
        if metadata.get("pdf_url"):
            file_urls.append(metadata["pdf_url"])
        file_urls.extend(metadata.get("media_urls") or [])

        article_bytes = 0
        downloaded_any = False
        for s3_url in file_urls:
            url = s3_url_to_https(s3_url)
            local_path = os.path.join(article_dir, os.path.basename(url))

            if os.path.exists(local_path):
                article_bytes += os.path.getsize(local_path)
                continue

            os.makedirs(article_dir, exist_ok=True)
            try:
                article_bytes += download_file(url, local_path, session)
                downloaded_any = True
            except requests.RequestException as e:
                print(f"  skip (network error): {url} ({e})")

        total += article_bytes
        if downloaded_any:
            kept += 1
            if kept % 25 == 0:
                print(f"  ... {kept} articles, {total / 1e9:.2f} GB so far")
        else:
            skipped_existing += 1

    print(f"\nDone. Downloaded {kept} new articles "
          f"(+{skipped_existing} already present), "
          f"total {total / 1e9:.2f} GB in {out_dir}/")


def main():
    ap = argparse.ArgumentParser(description=__doc__,
                                  formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--target-gb", type=float, default=1.0,
                     help="Stop once this many GB have been downloaded (default: 5)")
    ap.add_argument("--keyword", type=str, default=None,
                     help="Case-insensitive keyword to filter journal/citation text, "
                          "e.g. 'cancer', 'genomic', 'microbiome'")
    ap.add_argument("--license", choices=["all", "comm"], default="all",
                     help="'comm' restricts to the commercial-reuse-allowed subset")
    ap.add_argument("--out", type=str, default="./pmc_subset",
                     help="Output directory (default: ./pmc_subset)")
    ap.add_argument("--pmc-ids", type=str, default=DEFAULT_PMC_IDS_CSV,
                     help=f"Path to PMC-ids.csv (default: {DEFAULT_PMC_IDS_CSV})")
    args = ap.parse_args()

    target_bytes = args.target_gb * 1e9

    if args.keyword:
        print(f"Filtering for keyword: {args.keyword!r}")

    rows = iter_pmc_ids_rows(args.pmc_ids)
    download_subset(rows, args.out, target_bytes, args.license, args.keyword)


if __name__ == "__main__":
    main()