import sys
import time
import tkinter as tk
from tkinter import messagebox
import threading
from g_python.gextension import Extension
from g_python.hmessage import Direction

# --- CONFIGURATION ---
extension_info = {
    "title": "Origins Dice Hunter",
    "description": "Consolidated Sniffer & GUI",
    "version": "3.5",
    "author": "Nick"
}

# --- THE SNIFFER ENGINE ---
class DiceSniffer:
    def __init__(self, on_dice_found_callback):
        self.callback = on_dice_found_callback
        args = sys.argv if "-p" in sys.argv else ["-p", "9092"]
        self.ext = Extension(extension_info, args)

    def _intercept_handler(self, message):
        # Only capture packets going TO_SERVER (what you'll replay)
        if message.direction != Direction.TO_SERVER:
            return

        packet = message.packet
        full_g_string = packet.g_string(self.ext)

        try:
            packet_name = full_g_string.split('}{')[0].replace('{l}', '').replace('{u:', '').replace('}', '').upper()
            if packet_name.startswith("AL"):
                # Send both display value + raw packet string back to UI
                self.callback(packet_name, full_g_string)
        except Exception:
            pass

    def send_to_server(self, raw_packet: str):
        # Replays a previously captured packet to server
        self.ext.send_to_server(raw_packet)

    def start(self):
        self.ext.intercept(Direction.TO_SERVER, self._intercept_handler)
        self.ext.start()


# --- THE INTERFACE ---
class DiceApp:
    def __init__(self):
        self.root = tk.Tk()
        self.root.title("Dice Automation")
        self.root.geometry("400x350")
        self.root.attributes("-topmost", True)

        self.dice_count = 0
        self.found_ids = []
        self.captured_raw_packets = []  # store raw packets for replay

        self.label = tk.Label(self.root, text="Please roll 5 dice", font=("Arial", 14, "bold"), pady=10)
        self.label.pack()

        self.counter_label = tk.Label(self.root, text="Progress: 0/5", font=("Arial", 12))
        self.counter_label.pack()

        self.log_box = tk.Listbox(self.root, width=40, height=10, font=("Consolas", 10))
        self.log_box.pack(pady=10)

        self.reset_button = tk.Button(self.root, text="Reset Dice", command=self.reset_dice)
        self.reset_button.pack(pady=5)

        self.reroll_button = tk.Button(self.root, text="Reroll All Dice", command=self.reroll_all_dice)
        self.reroll_button.pack(pady=5)

        self.sniffer = DiceSniffer(self.handle_new_die)
        threading.Thread(target=self.sniffer.start, daemon=True).start()

    def handle_new_die(self, al_string, raw_packet):
        if self.dice_count < 5:
            self.dice_count += 1
            self.found_ids.append(al_string)
            self.captured_raw_packets.append(raw_packet)
            self.root.after(0, self.update_ui, al_string)

            if self.dice_count == 5:
                self.root.after(0, self.show_finished)

    def update_ui(self, al_string):
        self.log_box.insert(tk.END, f"Die {self.dice_count}: {al_string}")
        self.counter_label.config(text=f"Progress: {self.dice_count}/5")
        self.log_box.see(tk.END)

    def show_finished(self):
        self.label.config(text="Goal Reached!", fg="green")
        messagebox.showinfo("Success", "Successfully captured 5 dice rolls!")

    def reset_dice(self):
        self.dice_count = 0
        self.found_ids.clear()
        self.captured_raw_packets.clear()
        self.label.config(text="Please roll 5 dice", fg="black")
        self.counter_label.config(text="Progress: 0/5")
        self.log_box.delete(0, tk.END)

    def _replay_packets_thread(self):
        sent = 0
        for raw in self.captured_raw_packets:
            try:
                self.sniffer.send_to_server(raw)
                sent += 1
                time.sleep(0.08)  # small gap to avoid flooding
            except Exception:
                pass
        self.root.after(0, lambda: messagebox.showinfo("Reroll", f"Sent {sent} packet(s) back to server."))

    def reroll_all_dice(self):
        if not self.captured_raw_packets:
            messagebox.showwarning("Reroll", "No captured dice packets to replay.")
            return
        threading.Thread(target=self._replay_packets_thread, daemon=True).start()

    def run(self):
        self.root.mainloop()

if __name__ == "__main__":
    app = DiceApp()
    app.run()