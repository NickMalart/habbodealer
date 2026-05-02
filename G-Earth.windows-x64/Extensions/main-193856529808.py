import tkinter as tk
from tkinter import messagebox
import threading
from dice_tracker import DiceSniffer # This looks for dice_tracker.py in the same folder

class DiceApp:
    def __init__(self):
        self.root = tk.Tk()
        self.root.title("Dice Automation")
        self.root.geometry("400x350")
        self.root.attributes("-topmost", True)

        self.dice_count = 0
        self.found_ids = []

        self.label = tk.Label(self.root, text="Please roll 5 dice", font=("Arial", 14, "bold"), pady=10)
        self.label.pack()

        self.counter_label = tk.Label(self.root, text="Progress: 0/5", font=("Arial", 12))
        self.counter_label.pack()

        self.log_box = tk.Listbox(self.root, width=40, height=10, font=("Consolas", 10))
        self.log_box.pack(pady=10)

        # Start the sniffer from our other file in a background thread
        self.sniffer = DiceSniffer(self.handle_new_die)
        threading.Thread(target=self.sniffer.start, daemon=True).start()

    def handle_new_die(self, al_string):
        if self.dice_count < 5:
            self.dice_count += 1
            self.found_ids.append(al_string)
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

    def run(self):
        self.root.mainloop()

if __name__ == "__main__":
    app = DiceApp()
    app.run()