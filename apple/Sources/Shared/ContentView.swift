import SwiftUI

struct ContentView: View {
    @StateObject private var vpn = VPNManager()
    @State private var proxyJSON = SharedState.proxyJSON
    @State private var showingSettings = false

    var body: some View {
        ZStack {
            LinearGradient(colors: [Color.black, Color(red: 0.01, green: 0.08, blue: 0.06)], startPoint: .top, endPoint: .bottom).ignoresSafeArea()
            VStack(spacing: 28) {
                HStack {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("OrcheRoute").font(.title2.bold())
                        Text("TRAFFIC ORCHESTRATION").font(.caption).tracking(2).foregroundStyle(.secondary)
                    }
                    Spacer()
                    Button { showingSettings = true } label: { Image(systemName: "gearshape") }.buttonStyle(.bordered)
                }
                Spacer()
                Circle().fill(vpn.connected ? Color.mint : Color.gray.opacity(0.25)).frame(width: 220, height: 220)
                    .overlay(Button(vpn.connected ? "ВЫКЛЮЧИТЬ" : "ВКЛЮЧИТЬ") { Task { await vpn.toggle() } }.buttonStyle(.plain).font(.headline).foregroundStyle(vpn.connected ? .black : .white))
                    .shadow(color: vpn.connected ? .mint.opacity(0.35) : .clear, radius: 35)
                Text(vpn.statusText).font(.title3)
                if let error = vpn.error { Text(error).foregroundStyle(.red).multilineTextAlignment(.center) }
                Spacer()
                Text("Mihomo · общий Go runtime · Network Extension").font(.caption).foregroundStyle(.secondary)
            }.padding(28)
        }
        .foregroundStyle(.white)
        .sheet(isPresented: $showingSettings) {
            NavigationStack {
                Form {
                    Section("Активный сервер") {
                        TextEditor(text: $proxyJSON).font(.system(.caption, design: .monospaced)).frame(minHeight: 220)
                        Text("Временный нативный редактор JSON. Реестр подписок будет использовать тот же общий формат состояния.").font(.caption).foregroundStyle(.secondary)
                    }
                }
                .navigationTitle("Настройки")
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) { Button("Закрыть") { showingSettings = false } }
                    ToolbarItem(placement: .confirmationAction) { Button("Сохранить") { SharedState.suite.set(proxyJSON, forKey: SharedState.proxyKey); showingSettings = false } }
                }
            }.frame(minWidth: 560, minHeight: 440)
        }
    }
}
