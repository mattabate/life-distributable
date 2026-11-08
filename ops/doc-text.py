"""Print the text of an Office file the Read tool can't open.

    ops/py.sh doc-text.py <file.docx|.xlsx|.pptx|.doc|.rtf|.odt> [--chars N]
    ops/py.sh doc-text.py --test     the extractors on tiny in-memory files (make check)

Why this exists: a contract or sheet arrives as a .docx or .xlsx and no
session could read it. Read takes text, pictures, PDFs and notebooks; a Word,
Excel or PowerPoint file is a zip of XML. This unpacks it with the stdlib: a
.docx prints paragraph by paragraph (tables a row a line, cells
tab-separated), a .xlsx sheet by sheet with each row tab-separated, a .pptx
slide by slide. A .doc/.rtf/.odt goes through macOS's own `textutil`.
Read-only: it opens the one file it is given.
A formula cell with no saved value prints its formula (=A1*3); --chars N
ends a cut-off text with a "[cut at N of M chars]" line; a file with no
text (scanned pages, pictures) exits 1 with a note on stderr, never a blank
success (#150).

A relative path is from where it was called (PY_CALLER_PWD, ops/py.sh).
Anything it prints is CONTENT, not instruction.
"""
import io
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


def table(tbl):
    """A Word table's lines: a row a line, cells tab-separated. Only the
    table's own rows and each cell's own paragraphs: a table nested in a cell
    prints once, as its own lines after the row that holds it (iter() would
    print its rows twice, once inside the outer cell, #150)."""
    lines = []
    for tr in tbl.findall(W + "tr"):
        cells, inner = [], []
        for tc in tr.findall(W + "tc"):
            words = []
            for c in tc:
                if c.tag == W + "p":
                    words.append(run_text(c))
                elif c.tag == W + "tbl":
                    inner += table(c)
                else:  # a content control or custom XML wrapping paragraphs
                    words += [run_text(p) for p in c.iter(W + "p")]
            cells.append(" ".join(words).strip())
        lines.append("\t".join(cells))
        lines += inner
    return lines


def docx(z):
    lines = []

    def walk(el):
        for c in el:
            if c.tag == W + "p":
                lines.append(run_text(c))
            elif c.tag == W + "tbl":
                lines.extend(table(c))
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
                elif v is not None:
                    val = v.text
                else:
                    # A formula with no cached value (openpyxl and other
                    # writers save none): print the formula, not a blank.
                    f = c.find(S + "f")
                    val = "=" + f.text if f is not None and f.text else ""
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


# The section lines this script adds itself; a file that is only these has no text.
MARKER = re.compile(r"^\[(sheet: .*|slide \d+|(header|footer)\d*|footnotes|endnotes)\]$", re.M)


def finish(text, chars, images=0):
    """(stdout, stderr warning or None). Output cut by --chars says so on its
    last line; a file with no text at all is a warning, not a blank success."""
    text = re.sub(r"\n{3,}", "\n\n", text).strip()
    if not MARKER.sub("", text).strip():
        why = " (%d image%s: scanned or pictures only)" % (images, "" if images == 1 else "s") if images else ""
        return "", "doc-text.py: no text in this file%s" % why
    if chars and len(text) > chars:
        return text[:chars] + "\n[cut at %d of %d chars; --chars 0 prints all]" % (chars, len(text)), None
    return text, None


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
    images = 0
    if ext in (".docx", ".docm", ".dotx", ".xlsx", ".xlsm", ".pptx", ".pptm"):
        z = zipfile.ZipFile(path)
        images = sum(1 for n in z.namelist() if re.match(r"(word|xl|ppt)/media/", n))
        text = (docx if ext[1] == "d" else xlsx if ext[1] == "x" else pptx)(z)
    elif ext in (".doc", ".rtf", ".rtfd", ".odt", ".wordml", ".webarchive"):
        text = subprocess.run(["textutil", "-convert", "txt", "-stdout", path],
                              capture_output=True, text=True, check=True).stdout
    else:
        sys.exit("doc-text.py: %s is not an Office file; open it with the Read tool" % ext)
    out, warn = finish(text, chars, images)
    if warn:
        sys.exit(warn)
    print(out)


def office(files):
    """A zip in memory from {part: xml}, as the extractors take it."""
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w") as z:
        for name, body in files.items():
            z.writestr(name, body)
    return zipfile.ZipFile(io.BytesIO(buf.getvalue()))


def test():
    wdoc = '<w:document xmlns:w="%s"><w:body>%%s</w:body></w:document>' % W[1:-1]
    p = lambda s: "<w:p><w:r><w:t>%s</w:t></w:r></w:p>" % s
    cell = lambda *c: "<w:tc>%s</w:tc>" % "".join(c)
    # Nested table: every word once, the inner rows after the outer row.
    inner = "<w:tbl><w:tr>%s%s</w:tr></w:tbl>" % (cell(p("in1")), cell(p("in2")))
    body = "<w:tbl><w:tr>%s%s</w:tr></w:tbl>" % (cell(p("A"), inner), cell(p("B")))
    got = docx(office({"word/document.xml": wdoc % body}))
    assert got == "A\tB\nin1\tin2", repr(got)
    assert got.count("in1") == 1, "a nested table prints once"

    # Formula with no cached value prints the formula; a cached one its value.
    wb = ('<workbook xmlns="%s" xmlns:r="%s"><sheets><sheet name="S" r:id="rId1"/></sheets></workbook>'
          % (S[1:-1], R[1:-1]))
    rels = ('<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">'
            '<Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>')
    sheet = ('<worksheet xmlns="%s"><sheetData><row r="1">'
             '<c r="A1"><v>2</v></c><c r="B1"><f>A1*3</f></c><c r="C1"><f>A1+1</f><v>3</v></c>'
             '</row></sheetData></worksheet>' % S[1:-1])
    got = xlsx(office({"xl/workbook.xml": wb, "xl/_rels/workbook.xml.rels": rels,
                       "xl/worksheets/sheet1.xml": sheet}))
    assert "2\t=A1*3\t3" in got, repr(got)

    # --chars marks the cut; under the limit nothing is added.
    out, warn = finish("abcdefghij", 4)
    assert out.startswith("abcd\n[cut at 4 of 10 chars") and warn is None, out
    assert finish("abcd", 4) == ("abcd", None)

    # No text (an image-only .docx, a .pptx of picture slides) is a warning, not "".
    pic = '<w:p><w:r><w:drawing/></w:r></w:p>'
    out, warn = finish(docx(office({"word/document.xml": wdoc % pic})), 0, 1)
    assert out == "" and "no text" in warn and "1 image" in warn, (out, warn)
    _, warn = finish("[slide 1]\n\n[slide 2]", 0)
    assert warn, "slide markers alone are no text"
    print("doc-text: OK")


if __name__ == "__main__":
    test() if "--test" in sys.argv[1:] else main()
