"""Download an open-access article from PubMed Central by PMCID.

PubTator cannot serve this: its export endpoint rejects `pmcids` outright
("pmids is a mandatory parameter", HTTP 400), so downloads go straight to the
PMC Open Access S3 bucket that replaced the retired FTP service in Aug 2026:

    https://pmc-oa-opendata.s3.amazonaws.com/metadata/<PMCID>.<version>.json

That per-article metadata object carries the license, the OA flag, and s3://
URLs for the XML / plain text / PDF / supplementary media.
"""

from __future__ import annotations

import asyncio
import re
import xml.etree.ElementTree as ET
from pathlib import Path
from typing import Iterable, Optional

import httpx

from api.ncbi import http
from api.ncbi.errors import InvalidRequestError, NotFoundError, UpstreamError
from api.pm_client.models import DownloadedFile, DownloadResponse, FileKind

PMCID_RE = re.compile(r"^PMC\d+$")
ALL_KINDS: tuple[FileKind, ...] = ("xml", "text", "pdf", "media")
_S3_NS = {"s3": "http://s3.amazonaws.com/doc/2006-03-01/"}


def normalise_pmcid(pmcid: str) -> str:
    """Accept 'PMC107028' or '107028'; reject anything else.

    This value becomes a directory name, so the regex is also the path guard.
    """
    candidate = pmcid.strip().upper()
    if candidate.isdigit():
        candidate = f"PMC{candidate}"
    if not PMCID_RE.match(candidate):
        raise InvalidRequestError(f"not a PMCID: {pmcid!r}")
    return candidate


def _https_url(s3_url: str, bucket_base: str) -> str:
    """s3://pmc-oa-opendata/PMC1.1/PMC1.1.xml?md5=... -> https://<base>/PMC1.1/PMC1.1.xml"""
    key = s3_url.split("/", 3)[3].split("?", 1)[0]
    return f"{bucket_base}/{key}"


class PmcClient:
    def __init__(self, client: httpx.AsyncClient, bucket_base_url: str, papers_dir: Path) -> None:
        self._client = client
        self._base = bucket_base_url.rstrip("/")
        self._papers_dir = papers_dir

    async def _fetch_metadata(self, pmcid: str) -> dict:
        """Almost every article is version 1; fall back to an S3 listing otherwise."""
        response = await http.get(self._client, f"{self._base}/metadata/{pmcid}.1.json")
        if response.status_code == 200:
            return response.json()
        if response.status_code != 404:
            raise UpstreamError(f"PMC metadata for {pmcid} returned HTTP {response.status_code}")

        versioned = await self._find_versioned_prefix(pmcid)
        if versioned is None:
            raise NotFoundError(f"{pmcid} is not in the PMC open-access dataset")
        response = await http.get(self._client, f"{self._base}/metadata/{versioned}.json")
        if response.status_code != 200:
            raise NotFoundError(f"{pmcid} is not in the PMC open-access dataset")
        return response.json()

    async def _find_versioned_prefix(self, pmcid: str) -> Optional[str]:
        response = await http.get(
            self._client,
            f"{self._base}/",
            params={"list-type": "2", "prefix": f"{pmcid}.", "delimiter": "/"},
        )
        if response.status_code != 200:
            return None
        try:
            root = ET.fromstring(response.text)
        except ET.ParseError as exc:
            raise UpstreamError(f"malformed S3 listing for {pmcid}: {exc}") from exc
        prefixes = [
            p.findtext("s3:Prefix", namespaces=_S3_NS)
            for p in root.findall("s3:CommonPrefixes", _S3_NS)
        ]
        prefixes = [p for p in prefixes if p]
        return prefixes[0].rstrip("/") if prefixes else None

    @staticmethod
    def _planned_files(metadata: dict, kinds: Iterable[FileKind], base: str) -> list[tuple]:
        """(kind, https_url) for each requested file present in the metadata."""
        wanted = set(kinds)
        plan: list[tuple] = []
        for kind, key in (("xml", "xml_url"), ("text", "text_url"), ("pdf", "pdf_url")):
            if kind in wanted and metadata.get(key):
                plan.append((kind, _https_url(metadata[key], base)))
        if "media" in wanted:
            for url in metadata.get("media_urls") or []:
                plan.append(("media", _https_url(url, base)))
        return plan

    async def download(
        self,
        pmcid: str,
        *,
        kinds: Iterable[FileKind] = ALL_KINDS,
        dry_run: bool = False,
        overwrite: bool = False,
    ) -> DownloadResponse:
        pmcid = normalise_pmcid(pmcid)
        metadata = await self._fetch_metadata(pmcid)

        if not metadata.get("is_pmc_openaccess", False):
            raise NotFoundError(f"{pmcid} is in PMC but not open access; refusing to download")

        version = int(metadata.get("version", 1))
        result = DownloadResponse(
            pmcid=metadata.get("pmcid", pmcid),
            version=version,
            pmid=metadata.get("pmid"),
            doi=metadata.get("doi"),
            title=metadata.get("title"),
            citation=metadata.get("citation"),
            license_code=metadata.get("license_code"),
            is_open_access=True,
            is_retracted=bool(metadata.get("is_retracted")),
        )

        plan = self._planned_files(metadata, kinds, self._base)
        if dry_run:
            result.files = [
                DownloadedFile(kind=k, filename=u.rsplit("/", 1)[-1], url=u, bytes=0, skipped=True)
                for k, u in plan
            ]
            return result

        target = (self._papers_dir / pmcid).resolve()
        # Belt and braces: normalise_pmcid already forbids separators.
        if self._papers_dir.resolve() not in target.parents:
            raise InvalidRequestError(f"refusing to write outside the papers directory: {pmcid}")
        target.mkdir(parents=True, exist_ok=True)
        result.directory = str(target)

        # Serial, not gathered: NCBI asks for ~3 requests/second, and one article
        # can carry a long tail of media files.
        for kind, url in plan:
            result.files.append(await self._download_one(kind, url, target, overwrite))
        return result

    async def _download_one(
        self, kind: FileKind, url: str, target: Path, overwrite: bool
    ) -> DownloadedFile:
        filename = url.rsplit("/", 1)[-1]
        path = target / filename
        if path.exists() and not overwrite:
            return DownloadedFile(
                kind=kind, filename=filename, url=url, bytes=path.stat().st_size, skipped=True
            )

        # Stream to a temp file so an interrupted download can't be mistaken for
        # a complete one on the next run.
        tmp = path.with_suffix(path.suffix + ".part")
        written = 0
        try:
            async with self._client.stream("GET", url) as response:
                if response.status_code != 200:
                    raise UpstreamError(f"{url} returned HTTP {response.status_code}")
                with tmp.open("wb") as fh:
                    async for chunk in response.aiter_bytes(256 * 1024):
                        fh.write(chunk)
                        written += len(chunk)
        except httpx.HTTPError as exc:
            tmp.unlink(missing_ok=True)
            raise UpstreamError(f"{url} failed: {exc}") from exc
        except asyncio.CancelledError:
            tmp.unlink(missing_ok=True)
            raise
        tmp.replace(path)
        return DownloadedFile(kind=kind, filename=filename, url=url, bytes=written)
