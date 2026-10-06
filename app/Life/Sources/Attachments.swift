import SwiftUI
import PhotosUI
import Photos
import UIKit
import ImageIO
import UniformTypeIdentifiers
import QuickLook

/// A document (a PDF, a CSV, an export) attached to a message, as opposed to
/// a picture: the bytes go up untouched under their own name, so the hub
/// keeps the extension and the session Reads the file page by page instead
/// of looking at it. Comes from the Files picker in the
/// composer or from the share sheet of any app (LifeShare → SharedInbox).
struct AttachedFile: Identifiable, Sendable {
    let id = UUID()
    let name: String
    let data: Data
    var isPDF: Bool { name.lowercased().hasSuffix(".pdf") }
}

/// Photos and files attached to a session message (new-session sheet or the
/// in-thread composer). Each is uploaded as an `app/photo` (or `app/upload`)
/// observation first; the returned blob refs ride along with the message
/// (shared/api.md "Threads").
@Observable @MainActor
final class AttachmentDraft {
    var images: [UIImage] = []
    /// Parallel to `images`: PHAsset local identifier when the image came from
    /// the Photos library (nil for camera shots and in-app snapshots). Used to
    /// offer deleting screenshots from the library after they are sent.
    var assetIDs: [String?] = []
    /// Documents, after the pictures (their refs follow the pictures' too).
    var files: [AttachedFile] = []
    var uploading = false

    var isEmpty: Bool { images.isEmpty && files.isEmpty }
    var count: Int { images.count + files.count }

    /// What the File picker offers, and what the box takes dropped or pasted.
    nonisolated static let fileTypes: [UTType] = [.pdf, .image, .commaSeparatedText, .json, .plainText]

    func add(_ image: UIImage, assetID: String? = nil) { images.append(image); assetIDs.append(assetID) }
    func add(_ file: AttachedFile) { files.append(file) }
    func remove(at i: Int) { images.remove(at: i); if i < assetIDs.count { assetIDs.remove(at: i) } }
    func removeFile(at i: Int) { files.remove(at: i) }

    /// A picked file from the Files app (security-scoped URL): an image is a
    /// picture like any other, anything else rides as a document. Up to 16 MB
    /// — a year of bank statements is well under, and the tailnet link away
    /// from home is slow.
    func add(fileAt url: URL) {
        let scoped = url.startAccessingSecurityScopedResource()
        defer { if scoped { url.stopAccessingSecurityScopedResource() } }
        guard let data = try? Data(contentsOf: url), data.count <= 16 << 20 else { return }
        add(AttachedFile(name: url.lastPathComponent, data: data), picturesAsImages: true)
    }

    /// A file that arrived by name (picked, dropped on the box, pasted from
    /// Finder): a picture joins the pictures, anything else stays a document.
    func add(_ file: AttachedFile, picturesAsImages: Bool) {
        let ext = (file.name as NSString).pathExtension
        if picturesAsImages, let type = UTType(filenameExtension: ext), type.conforms(to: .image), let img = UIImage(data: file.data) {
            add(img)
        } else {
            add(file)
        }
    }

    /// Everything in the box, moved out of it: what a send uploads while the
    /// composer is already empty for the next message. Documents travel with
    /// the pictures — the session composer's send copied `images` alone, so a
    /// CSV whose chip the owner could see never left the Mac.
    func take() -> AttachmentDraft {
        let out = AttachmentDraft()
        out.images = images; out.assetIDs = assetIDs; out.files = files
        clear()
        return out
    }

    /// A failed send puts them back, unless something was attached since.
    func restore(_ taken: AttachmentDraft) {
        guard isEmpty else { return }
        images = taken.images; assetIDs = taken.assetIDs; files = taken.files
    }

    /// Everything dropped that is a file, into the box: a picture joins the
    /// pictures, anything else is a chip. True when there was a file to take.
    @discardableResult
    func take(dropped providers: [NSItemProvider]) -> Bool {
        let files = providers.filter(Self.isFile)
        for p in files {
            Self.loadFile(p) { [weak self] f in if let f { self?.add(f, picturesAsImages: true) } }
        }
        return !files.isEmpty
    }

    /// Is this dropped or pasted item a FILE (a CSV out of Finder, a PDF from
    /// the Files app) rather than words? A text view takes any file whose
    /// type is text and types its contents into the draft — a 3 KB statement
    /// became the message itself. A file names itself: it
    /// is a file URL, or it carries a name and is not a run of copied text.
    nonisolated static func isFile(_ p: NSItemProvider) -> Bool {
        let types = p.registeredTypeIdentifiers
        if types.contains(UTType.fileURL.identifier) { return true }
        guard let name = p.suggestedName, !name.isEmpty else { return false }
        return !types.contains(UTType.utf8PlainText.identifier) && !types.contains(UTType.url.identifier)
    }

    /// The bytes and the name of a dropped or pasted file, same 16 MB bound
    /// as a picked one. The load starts before this returns (a drop's
    /// providers are only good inside the drop); `done` runs on the main
    /// actor, with nil when there was nothing to read.
    nonisolated static func loadFile(_ p: NSItemProvider, done: @escaping @MainActor @Sendable (AttachedFile?) -> Void) {
        let finish: @Sendable (AttachedFile?) -> Void = { f in Task { @MainActor in done(f) } }
        let read: @Sendable (URL, String?) -> AttachedFile? = { url, named in
            let scoped = url.startAccessingSecurityScopedResource()
            defer { if scoped { url.stopAccessingSecurityScopedResource() } }
            guard let data = try? Data(contentsOf: url), !data.isEmpty, data.count <= 16 << 20 else { return nil }
            // The Files app names a dragged file without its extension; the
            // copy it hands over keeps it.
            var name = named ?? url.lastPathComponent
            if (name as NSString).pathExtension.isEmpty, !url.pathExtension.isEmpty { name += "." + url.pathExtension }
            return AttachedFile(name: name, data: data)
        }
        if p.registeredTypeIdentifiers.contains(UTType.fileURL.identifier) {
            // Finder on the Mac: the file itself, where it lives.
            _ = p.loadObject(ofClass: URL.self) { url, _ in finish(url.flatMap { read($0, nil) }) }
            return
        }
        guard let type = p.registeredTypeIdentifiers.first else { finish(nil); return }
        let named = p.suggestedName
        // The copy is deleted when this handler returns: read it inside.
        p.loadFileRepresentation(forTypeIdentifier: type) { url, _ in finish(url.flatMap { read($0, named) }) }
    }

    /// Downscale to ~1600px max side, JPEG 0.7 (~100–400 KB): enough for Claude
    /// to read a receipt or a meal, small enough for the tailnet. 1600 is not
    /// arbitrary — the model downsamples anything above ~1568px on its long
    /// edge, so every pixel past that is bytes the user waits for and nobody reads.
    ///
    /// Two things here are load-bearing:
    ///
    /// 1. `image.size` is in POINTS. An in-app snap comes back at scale 3, so
    ///    a 1320×2868-pixel screen measured 440×956 and the cap never bit.
    ///    Measure the PIXELS.
    /// 2. `UIGraphicsImageRenderer`'s default format takes the SCREEN's scale
    ///    — often 3 — so it would render `size` points at 3× and the JPEG
    ///    would come out at nine times the pixels asked for (a 5 MB photo
    ///    instead of ~450 KB: a long send, then a long download to draw a
    ///    220 pt bubble).
    ///
    /// `nonisolated` so the caller can do this off the main actor: it is a full
    /// decode, redraw and JPEG encode of a 12 MP photo, and on the main actor
    /// it froze the very spinner that is meant to say the send is alive.
    nonisolated static func jpeg(_ image: UIImage) -> (Data, CGSize)? {
        let maxSide: CGFloat = 1600
        let px = CGSize(width: image.size.width * image.scale, height: image.size.height * image.scale)
        guard px.width > 0, px.height > 0 else { return nil }
        let shrink = min(1, maxSide / max(px.width, px.height))
        let size = CGSize(width: (px.width * shrink).rounded(), height: (px.height * shrink).rounded())
        let fmt = UIGraphicsImageRendererFormat.preferred()
        fmt.scale = 1          // `size` is already in pixels — never re-apply the screen scale
        fmt.opaque = true      // JPEG has no alpha; an opaque context is cheaper
        let scaled = UIGraphicsImageRenderer(size: size, format: fmt).image { _ in image.draw(in: CGRect(origin: .zero, size: size)) }
        guard let d = scaled.jpegData(compressionQuality: 0.7) else { return nil }
        return (d, size)
    }

    /// Upload every image; returns blob refs in order. Throws on the first failure
    /// (nothing is sent to the session in that case, so the user can retry).
    ///
    /// Compress off the main actor, then upload the photos AT ONCE rather than
    /// one after another: away from home the hub is a relayed tailnet link
    /// where each serial round trip is a wait the user sits through with the
    /// spinner spinning.
    func upload(to hub: HubClient, threadID: String?) async throws -> [String] {
        uploading = true
        defer { uploading = false }
        let imgs = images
        let docs = files
        let jobs: [(Data, CGSize)] = await Task.detached(priority: .userInitiated) {
            imgs.compactMap { Self.jpeg($0) }
        }.value
        return try await withThrowingTaskGroup(of: (Int, String).self) { group in
            for (i, job) in jobs.enumerated() {
                group.addTask { (i, try await hub.uploadSessionPhoto(job.0, threadID: threadID, size: job.1)) }
            }
            for (i, doc) in docs.enumerated() {
                group.addTask { (jobs.count + i, try await hub.uploadSessionFile(doc.data, name: doc.name, threadID: threadID)) }
            }
            var refs = [String](repeating: "", count: jobs.count + docs.count)
            for try await (i, ref) in group { refs[i] = ref }
            return refs
        }
    }

    func clear() { images = []; assetIDs = []; files = [] }
}

/// A document chip: the icon, the name, a ✕. Drawn in the composer strip and
/// as the first thing on the new-session sheet.
struct FileChip: View {
    let file: AttachedFile
    var remove: (() -> Void)? = nil
    var body: some View {
        HStack(spacing: 6) {
            Image(systemName: file.isPDF ? "doc.richtext" : "doc").font(.title3).foregroundStyle(.secondary)
            VStack(alignment: .leading, spacing: 1) {
                Text(file.name).font(.caption.weight(.semibold)).lineLimit(1).truncationMode(.middle)
                Text(Fmt.bytes(file.data.count)).font(.caption2).foregroundStyle(.secondary)
            }
            if let remove {
                Button(action: remove) {
                    Image(systemName: "xmark.circle.fill").font(.body).foregroundStyle(.secondary)
                }.accessibilityLabel("Remove \(file.name)")
            }
        }
        .padding(.horizontal, 10).padding(.vertical, 6)
        .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 10))
        .frame(maxWidth: 240)
    }
}

/// Library hygiene: screenshots taken to feed sessions litter the camera
/// roll. After a message with library photos is sent, the
/// *screenshots* among them (PHAsset subtype, so meals/real photos never
/// trigger this) are offered for deletion. iOS itself shows the "Delete N
/// screenshots?" confirmation — Don't Allow is always one tap away and the
/// app never deletes anything without that system dialog.
enum PhotoCleanup {
    static func offerDeletingScreenshots(_ ids: [String?]) async {
        let ids = ids.compactMap { $0 }
        guard !ids.isEmpty else { return }
        let status = await PHPhotoLibrary.requestAuthorization(for: .readWrite)
        guard status == .authorized || status == .limited else { return }
        let fetched = PHAsset.fetchAssets(withLocalIdentifiers: ids, options: nil)
        var shots: [PHAsset] = []
        fetched.enumerateObjects { a, _, _ in if a.mediaSubtypes.contains(.photoScreenshot) { shots.append(a) } }
        guard !shots.isEmpty else { return }
        try? await PHPhotoLibrary.shared().performChanges { PHAssetChangeRequest.deleteAssets(shots as NSArray) }
    }
}

/// Camera + Library buttons and a thumbnail strip. Compact enough for a composer row.
struct AttachBar: View {
    @Bindable var draft: AttachmentDraft
    var compact = false
    @State private var pickerItems: [PhotosPickerItem] = []
    @State private var showCamera = false

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            if !draft.isEmpty {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: 8) {
                        ForEach(Array(draft.images.enumerated()), id: \.offset) { i, img in
                            Image(uiImage: img).resizable().scaledToFill()
                                .frame(width: 72, height: 72).clipShape(RoundedRectangle(cornerRadius: 10))
                                #if targetEnvironment(macCatalyst)
                                // A click on the chip opens the picture over
                                // the window, as a click on a sent picture
                                // does (BlobImage → MacLightbox); the ✕ above
                                // it keeps its own click.
                                .contentShape(RoundedRectangle(cornerRadius: 10))
                                .onTapGesture { MacLightbox.open(image: img, name: "picture \(i + 1).jpg") }
                                .help("Click to see it at full size")
                                #endif
                                .overlay(alignment: .topTrailing) {
                                    Button { draft.remove(at: i) } label: {
                                        Image(systemName: "xmark.circle.fill").font(.body).foregroundStyle(.white, .black.opacity(0.6))
                                    }.padding(3)
                                }
                        }
                        ForEach(Array(draft.files.enumerated()), id: \.element.id) { i, f in
                            FileChip(file: f) { draft.removeFile(at: i) }
                        }
                    }.padding(.vertical, 2)
                }
            }
            if !compact {
                HStack {
                    Button { showCamera = true } label: { Label("Camera", systemImage: "camera") }
                        .disabled(!UIImagePickerController.isSourceTypeAvailable(.camera))
                    PhotosPicker(selection: $pickerItems, maxSelectionCount: 6, matching: .images, photoLibrary: .shared()) { Label("Library", systemImage: "photo.on.rectangle") }
                }.buttonStyle(.bordered)
            }
        }
        .fullScreenCover(isPresented: $showCamera) {
            CameraPicker(image: Binding(get: { nil }, set: { if let img = $0 { draft.add(img) } })).ignoresSafeArea()
        }
        .onChange(of: pickerItems) { _, items in
            guard !items.isEmpty else { return }
            Task { await draft.load(items); pickerItems = [] }
        }
    }

    /// Menu-style trigger for the composer: paperclip → Camera / Library / Paste.
    struct Menu: View {
        @Bindable var draft: AttachmentDraft
        @State private var pickerItems: [PhotosPickerItem] = []
        @State private var showCamera = false
        @State private var showLibrary = false
        @State private var showFiles = false
        #if targetEnvironment(macCatalyst)
        /// The desktop composer's bar draws this as the console's "Attach"
        /// button (a word in a hairline box) instead of the plus (the owner
        /// 2026-09-29: the desktop chat is the web chat).
        var label: String? = nil
        #endif
        var body: some View {
            SwiftUI.Menu {
                Button { showCamera = true } label: { Label("Take photo", systemImage: "camera") }
                    .disabled(!UIImagePickerController.isSourceTypeAvailable(.camera))
                Button { showLibrary = true } label: { Label("Photo library", systemImage: "photo.on.rectangle") }
                // The Files app: a PDF a bank app saved, a CSV export, a
                // screenshot the user filed — the session Reads it as a document.
                // The other road to the same place is the share sheet of the
                // app that has the file (LifeShare).
                Button { showFiles = true } label: { Label("File", systemImage: "doc") }
                // The clipboard road: the box itself takes ⌘V / long-press →
                // Paste, and here it is for a thumb with no keyboard. Only
                // offered when the clipboard holds a picture — `hasImages` is
                // a type check; `images` is read below, which is what makes
                // iOS ask "Allow Paste?", after the user chose it.
                if UIPasteboard.general.hasImages {
                    Button {
                        for img in UIPasteboard.general.images ?? [] { draft.add(img) }
                    } label: { Label("Paste picture", systemImage: "doc.on.clipboard") }
                }
            } label: {
                #if targetEnvironment(macCatalyst)
                if let label { WebSmallButton(text: label) } else {
                    Image(systemName: draft.isEmpty ? "plus.circle" : "plus.circle.fill").font(.system(size: 26))
                }
                #else
                Image(systemName: draft.isEmpty ? "plus.circle" : "plus.circle.fill").font(.system(size: 26))
                #endif
            }
            .accessibilityLabel("Attach photo")
            #if targetEnvironment(macCatalyst)
            // One plus, no chevron beside it (the owner 2026-09-29: "just a plus
            // button, not a plus button and a dropdown").
            .menuStyle(.button).buttonStyle(.plain).menuIndicator(.hidden).fixedSize()
            #endif
            .fullScreenCover(isPresented: $showCamera) {
                CameraPicker(image: Binding(get: { nil }, set: { if let img = $0 { draft.add(img) } })).ignoresSafeArea()
            }
            .photosPicker(isPresented: $showLibrary, selection: $pickerItems, maxSelectionCount: 6, matching: .images, photoLibrary: .shared())
            .fileImporter(isPresented: $showFiles, allowedContentTypes: AttachmentDraft.fileTypes, allowsMultipleSelection: true) { result in
                for url in (try? result.get()) ?? [] { draft.add(fileAt: url) }
            }
            .onChange(of: pickerItems) { _, items in
                guard !items.isEmpty else { return }
                Task { await draft.load(items); pickerItems = [] }
            }
        }
    }
}

extension AttachmentDraft {
    /// Load picked library items, remembering their asset ids (available because
    /// the pickers are bound to `.shared()`).
    func load(_ items: [PhotosPickerItem]) async {
        for item in items {
            if let d = try? await item.loadTransferable(type: Data.self), let img = UIImage(data: d) { add(img, assetID: item.itemIdentifier) }
        }
    }
}

/// Renders a hub blob (authenticated fetch + in-memory cache). Blobs are immutable.
struct BlobImage: View {
    @Environment(HubClient.self) private var hub
    let ref: String
    var maxHeight: CGFloat = 220
    @State private var image: UIImage?
    @State private var failed = false

    fileprivate static let cache = NSCache<NSString, UIImage>()

    var body: some View {
        Group {
            if let image {
                Image(uiImage: image).resizable().scaledToFit()
            } else if failed {
                Label("photo unavailable", systemImage: "photo.badge.exclamationmark").font(.caption).foregroundStyle(.secondary).padding(8)
            } else {
                ProgressView().frame(width: 80, height: 80)
            }
        }
        .frame(maxHeight: maxHeight)
        .clipShape(RoundedRectangle(cornerRadius: 10))
        #if targetEnvironment(macCatalyst)
        // The console's click-to-enlarge (threads.js openImage): the whole
        // file over the whole window, Esc or a click closes (MacLightbox).
        .contentShape(Rectangle())
        .onTapGesture { if image != nil { MacLightbox.open(ref: ref, hub: hub) } }
        #endif
        .task(id: ref) {
            if let c = Self.cache.object(forKey: ref as NSString) { image = c; return }
            #if targetEnvironment(macCatalyst)
            // On the Mac the cell draws up to `maxHeight` points tall on a 2×
            // screen, and a wide screenshot (2880×1800 at 180 pt = 288 pt
            // across) needs its WIDTH sharp: decode to the pixels it will
            // draw at, not `maxHeight × 3` on the longest side, which left a
            // wide picture at 540 px wide for a 576 px draw and a 220 pt cell
            // blurry (the owner 2026-09-30: "it's also low quality within the
            // context of the cells").
            let scale = max(2, UIScreen.main.scale)
            guard let d = try? await hub.blob(ref),
                  let img = await Self.thumbnail(d, height: maxHeight, scale: scale) else { failed = true; return }
            Self.cache.setObject(img, forKey: ref as NSString)
            image = img
            MacLightbox.openIfShotAsks(ref: ref, hub: hub)
            #else
            guard let d = try? await hub.blob(ref),
                  let img = await Self.thumbnail(d, max: Int(maxHeight * 3)) else { failed = true; return }
            Self.cache.setObject(img, forKey: ref as NSString)
            image = img
            #endif
        }
    }

    #if targetEnvironment(macCatalyst)
    /// The pixels a picture `pt` points tall needs on a `scale` screen: its
    /// height in pixels, or its width when it is wider than tall — the
    /// longest side is what `kCGImageSourceThumbnailMaxPixelSize` caps.
    /// Reads the file's own dimensions (and EXIF orientation, which swaps
    /// them) first; never decodes past the file's real size.
    nonisolated fileprivate static func thumbnail(_ data: Data, height pt: CGFloat, scale: CGFloat) async -> UIImage? {
        await Task.detached(priority: .userInitiated) {
            guard let src = CGImageSourceCreateWithData(data as CFData, nil) else { return nil }
            let props = CGImageSourceCopyPropertiesAtIndex(src, 0, nil) as? [CFString: Any]
            var w = CGFloat(props?[kCGImagePropertyPixelWidth] as? Int ?? 0)
            var h = CGFloat(props?[kCGImagePropertyPixelHeight] as? Int ?? 0)
            if let o = props?[kCGImagePropertyOrientation] as? UInt32, o >= 5 { swap(&w, &h) }
            let aspect = (w > 0 && h > 0) ? w / h : 1
            let need = Int((pt * scale * max(1, aspect)).rounded(.up))
            let opts: [CFString: Any] = [
                kCGImageSourceCreateThumbnailFromImageAlways: true,
                kCGImageSourceCreateThumbnailWithTransform: true,
                kCGImageSourceShouldCacheImmediately: true,
                kCGImageSourceThumbnailMaxPixelSize: min(max(need, 1), Int(max(w, h, 1))),
            ]
            guard let cg = CGImageSourceCreateThumbnailAtIndex(src, 0, opts as CFDictionary) else { return nil }
            return UIImage(cgImage: cg)
        }.value
    }
    #endif

    /// A phone photo arrives around 2200×4800. `UIImage(data:)` is cheap but
    /// lies: it decodes lazily, on whatever thread draws it — the main one,
    /// mid-scroll, at twenty times the pixels a 220 pt bubble can show. So
    /// decode once here, downsampled, off the main actor.
    nonisolated fileprivate static func thumbnail(_ data: Data, max px: Int) async -> UIImage? {
        await Task.detached(priority: .userInitiated) {
            let opts: [CFString: Any] = [
                kCGImageSourceCreateThumbnailFromImageAlways: true,
                kCGImageSourceCreateThumbnailWithTransform: true,   // honour the EXIF rotation
                kCGImageSourceShouldCacheImmediately: true,         // pay for the decode here, not at draw
                kCGImageSourceThumbnailMaxPixelSize: max(px, 1),
            ]
            guard let src = CGImageSourceCreateWithData(data as CFData, nil),
                  let cg = CGImageSourceCreateThumbnailAtIndex(src, 0, opts as CFDictionary) else { return nil }
            return UIImage(cgImage: cg)
        }.value
    }
}

/// Is this blob ref a picture? The hub stores a blob under its upload's own
/// extension, and the same list decides on the hub (threads.isImageBlob)
/// and in the console (threads.js isImageRef).
func isImageRef(_ ref: String) -> Bool {
    let ext = (ref as NSString).pathExtension.lowercased()
    return ["png", "jpg", "jpeg", "gif", "webp", "heic", "heif", "avif"].contains(ext)
}

/// A picture is drawn, anything else (a PDF shared from another app) is a
/// chip that opens in Quick Look — the two roads a chat attachment takes,
/// decided by the ref alone.
struct BlobAttachment: View {
    let ref: String
    var maxHeight: CGFloat = 240
    var body: some View {
        if isImageRef(ref) { BlobImage(ref: ref, maxHeight: maxHeight) } else { BlobDocument(ref: ref) }
    }
}

/// A document attachment in a chat: the file's name and a tap that fetches
/// the blob once and shows it in Quick Look (PDF, CSV, text — whatever iOS
/// can preview).
struct BlobDocument: View {
    @Environment(HubClient.self) private var hub
    let ref: String
    @State private var local: URL?
    @State private var fetching = false
    @State private var failed = false

    var name: String { String(ref.split(separator: "/").last ?? "file") }

    var body: some View {
        Button {
            if let local { preview = local } else { Task { await fetch() } }
        } label: {
            HStack(spacing: 8) {
                if fetching { ProgressView().controlSize(.small) }
                else { Image(systemName: failed ? "doc.badge.exclamationmark" : "doc.richtext").font(.title3) }
                Text(name).font(.caption.weight(.semibold)).lineLimit(1).truncationMode(.middle)
                Image(systemName: "arrow.up.forward.square").font(.caption).foregroundStyle(.secondary)
            }
            .padding(.horizontal, 10).padding(.vertical, 8)
            .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 10))
        }
        .buttonStyle(.plain)
        .quickLookPreview($preview)
        .accessibilityLabel("Open \(name)")
    }
    @State private var preview: URL?

    private func fetch() async {
        fetching = true; defer { fetching = false }
        guard let d = try? await hub.blob(ref) else { failed = true; return }
        // Quick Look wants a file with the real name; blobs are immutable, so
        // one copy per ref in the temp dir is right for the app's lifetime.
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent("blobs", isDirectory: true)
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let url = dir.appendingPathComponent(name)
        guard (try? d.write(to: url, options: .atomic)) != nil else { failed = true; return }
        local = url; preview = url
    }
}

/// A face: the same authenticated, cached blob fetch as `BlobImage`, drawn as a
/// circle at a fixed diameter with initials in the same circle until (or
/// unless) the picture arrives — so a person with no photo leaves the layout
/// exactly where it was.
struct BlobAvatar: View {
    @Environment(HubClient.self) private var hub
    let ref: String?
    let initials: String
    var size: CGFloat = 40
    @State private var image: UIImage?

    var body: some View {
        Group {
            if let image {
                Image(uiImage: image).resizable().scaledToFill()
            } else {
                Text(initials)
                    .font(.system(size: size * 0.36, weight: .semibold))
                    .foregroundStyle(.tint)
                    .frame(width: size, height: size)
                    .background(.tint.opacity(0.14))
            }
        }
        .frame(width: size, height: size)
        .clipShape(Circle())
        .task(id: ref) {
            guard let ref else { return }
            if let c = BlobImage.cache.object(forKey: ref as NSString) { image = c; return }
            guard let d = try? await hub.blob(ref),
                  let img = await BlobImage.thumbnail(d, max: Int(size * 3)) else { return }
            BlobImage.cache.setObject(img, forKey: ref as NSString)
            image = img
        }
    }
}

#if targetEnvironment(macCatalyst)
extension View {
    /// A file dragged out of Finder lands anywhere on the chat — the empty
    /// "Ready when you are." pane, a session's messages, the composer — not
    /// only on the bar along the bottom, as the console's whole messages pane
    /// takes a drop (composer.js wireDrop). the owner 2026-10-05 23:17: two
    /// bank statements dragged onto the chat, "it won't let me". A drop of
    /// words stays words: only files are taken.
    func dropsFiles(into attachments: AttachmentDraft) -> some View {
        onDrop(of: [UTType.fileURL.identifier] + AttachmentDraft.fileTypes.map(\.identifier), isTargeted: nil) { providers in
            attachments.take(dropped: providers)
        }
    }
}
#endif
