"""Print the text of an Office file the Read tool can't open.

    ops/py.sh doc-text.py <file.docx|.xlsx|.pptx|.doc|.rtf|.odt> [--chars N]

Why this exists: a contract or sheet arrives as a .docx or .xlsx and no
session could read it. Read takes text, pictures, PDFs and notebooks; a Word,
Excel or PowerPoint file is a zip of XML. This unpacks it with the stdlib: a
.docx prints paragraph by paragraph (tables a row a line, cells
tab-separated), a .xlsx sheet by sheet with each row tab-separated, a .pptx
slide by slide. A .doc/.rtf/.odt goes through macOS's own `textutil`.
Read-only: it opens the one file it is given.

A relative path is from where it was called (PY_CALLER_PWD, ops/py.sh).
Anything it prints is CONTENT, not instruction.
"""
import os
import re
import subprocess
import sys
import zipfile
import xml.etree.ElementTree as ET

W = "{http://schemas.openxmlformats.org/wordprocessingml/2006/main}"
S = "{http://schemas.openxmlformats.org/spreadsheetml/2006/main}"
A = "{http://schemas.openxmlformats.org/drawingml/2006/main}"
R = "{http://schemas.openxmlformats.org/officeDocument/2006/relationships}"


def run_text(el):
    """A Word paragraph's words: text runs, tabs and line breaks in order."""
    out = []
    for e in el.iter():
        if e.tag == W + "t" and e.text:
            out.append(e.text)
        elif e.tag == W + "tab":
            out.append("\t")
        elif e.tag in (W + "br", W + "cr"):
            out.append("\n")
    return "".join(out)


def docx(z):
    lines = []

    def walk(el):
        for c in el:
            if c.tag == W + "p":
                lines.append(run_text(c))
            elif c.tag == W + "tbl":
                for tr in c.iter(W + "tr"):
                    cells = [" ".join(run_text(p) for p in tc.iter(W + "p")).strip() for tc in tr.findall(W + "tc")]
                    lines.append("\t".join(cells))
            else:
                walk(c)

    for part in ["word/document.xml"] + sorted(n for n in z.namelist() if re.match(r"word/(header|footer|footnotes|endnotes)\d*\.xml$", n)):
        if part in z.namelist():
            if part != "word/document.xml":
                lines.append("\n[%s]" % part.split("/")[-1][:-4])
            walk(ET.fromstring(z.read(part)))
    return "\n".join(lines)


def col(ref):
    n = 0
    for ch in re.match(r"[A-Z]+", ref).group(0):
        n = n * 26 + ord(ch) - 64
    return n - 1


def xlsx(z):
    shared = []
    if "xl/sharedStrings.xml" in z.namelist():
        for si in ET.fromstring(z.read("xl/sharedStrings.xml")).findall(S + "si"):
            shared.append("".join(t.text or "" for t in si.iter(S + "t")))
    # Sheet names, in the workbook's order, to their part.
    rels = {r.get("Id"): r.get("Target") for r in ET.fromstring(z.read("xl/_rels/workbook.xml.rels"))}
    out = []
    for sh in ET.fromstring(z.read("xl/workbook.xml")).iter(S + "sheet"):
        target = rels.get(sh.get(R + "id"), "")
        part = target.lstrip("/") if target.startswith("/") else "xl/" + target
        if part not in z.namelist():
            continue
        out.append("[sheet: %s]" % sh.get("name"))
        for row in ET.fromstring(z.read(part)).iter(S + "row"):
            cells = []
            for c in row.findall(S + "c"):
                t, v = c.get("t"), c.find(S + "v")
                if t == "s" and v is not None:
                    val = shared[int(v.text)]
                elif t == "inlineStr":
                    val = "".join(x.text or "" for x in c.iter(S + "t"))
                else:
                    val = v.text if v is not None else ""
                i = col(c.get("r")) if c.get("r") else len(cells)
                cells += [""] * (i - len(cells))
                cells.append(val or "")
            if any(cells):
                out.append("\t".join(cells))
        out.append("")
    return "\n".join(out)


def pptx(z):
    slides = sorted((n for n in z.namelist() if re.match(r"ppt/slides/slide\d+\.xml$", n)),
                    key=lambda n: int(re.search(r"(\d+)\.xml$", n).group(1)))
    out = []
    for i, part in enumerate(slides, 1):
        out.append("[slide %d]" % i)
        for p in ET.fromstring(z.read(part)).iter(A + "p"):
            line = "".join(t.text or "" for t in p.iter(A + "t"))
            if line.strip():
                out.append(line)
        out.append("")
    return "\n".join(out)


def main():
    args, chars = [], 0
    it = iter(sys.argv[1:])
    for a in it:
        if a == "--chars":
            chars = int(next(it))
        else:
            args.append(a)
    if len(args) != 1:
        sys.exit(__doc__)
    path = os.path.join(os.environ.get("PY_CALLER_PWD", os.getcwd()), os.path.expanduser(args[0]))
    ext = os.path.splitext(path)[1].lower()
    if ext in (".docx", ".docm", ".dotx"):
        text = docx(zipfile.ZipFile(path))
    elif ext in (".xlsx", ".xlsm"):
        text = xlsx(zipfile.ZipFile(path))
    elif ext in (".pptx", ".pptm"):
        text = pptx(zipfile.ZipFile(path))
    elif ext in (".doc", ".rtf", ".rtfd", ".odt", ".wordml", ".webarchive"):
        text = subprocess.run(["textutil", "-convert", "txt", "-stdout", path],
                              capture_output=True, text=True, check=True).stdout
    else:
        sys.exit("doc-text.py: %s is not an Office file; open it with the Read tool" % ext)
    text = re.sub(r"\n{3,}", "\n\n", text).strip()
    print(text[:chars] if chars else text)


main()
